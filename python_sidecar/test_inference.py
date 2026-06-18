"""
End-to-end test for the CLAP sidecar — mirrors the EXACT Go worker pipeline.

Go worker does:
  1. audio.StreamAudioFromURL  → download MP3 bytes from IA
  2. audio.DecodeMP3           → PCM float64 at native rate (typically 44100Hz)
  3. clap.Float32ToBytes       → little-endian float32 bytes
  4. clapClient.GetEmbedding   → gRPC to sidecar with real sample rate
  5. sidecar resamples to 48000, runs CLAP, returns 512-dim embedding

This test replicates that pipeline with a real IA audio file against
a locally running CLAP model.

    make test-sidecar
"""

import struct
import subprocess
import sys
import time
import socket

import grpc
import httpx
import miniaudio
import numpy as np

sys.path.insert(0, ".")
import clap_pb2
import clap_pb2_grpc

IA_AUDIO_URLS = [
    "https://archive.org/download/testmp3testfile/mpthreetest.mp3",
    "https://archive.org/download/78_take-the-a-train_duke-ellington-and-his-famous-orchestra-strayhorn_gbia0060012a/Take%20the%20A%20Train%20-%20Duke%20Ellington%20and%20His%20Famous%20Orchestra-restored.mp3",
]
MAX_MSG = 50 * 1024 * 1024


def download_ia_mp3(max_bytes=1_600_000):
    with httpx.Client(follow_redirects=True, timeout=30) as client:
        for url in IA_AUDIO_URLS:
            try:
                resp = client.get(
                    url,
                    headers={
                        "User-Agent": "timbre-test/1.0",
                        "Range": f"bytes=0-{max_bytes - 1}",
                    },
                )
                if resp.status_code in (200, 206) and len(resp.content) > 1000:
                    return resp.content, url.split("/")[-1]
            except Exception:
                continue
    raise RuntimeError("could not download any IA audio sample")


def decode_mp3_native(mp3_bytes):
    """Decode MP3 at native sample rate (matches Go's audio.DecodeMP3)."""
    decoded = miniaudio.decode(
        mp3_bytes,
        output_format=miniaudio.SampleFormat.FLOAT32,
        nchannels=1,
    )
    pcm = np.frombuffer(decoded.samples, dtype=np.float32)
    return pcm, decoded.sample_rate


def float32_to_bytes(samples):
    """Pack float32 samples as little-endian bytes (matches Go's clap.Float32ToBytes)."""
    return np.asarray(samples, dtype=np.float32).tobytes()


def wait_for_port(host, port, timeout=120):
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            s = socket.create_connection((host, port), timeout=2)
            s.close()
            return True
        except OSError:
            time.sleep(2)
    return False


def test_worker_pipeline():
    """Full Go-worker-equivalent pipeline: IA MP3 -> decode -> gRPC -> CLAP -> 512 dims."""
    proc = subprocess.Popen(
        [sys.executable, "server.py"],
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )
    try:
        assert wait_for_port("localhost", 50051), "sidecar did not start within 120s"

        channel = grpc.insecure_channel(
            "localhost:50051",
            options=[
                ("grpc.max_send_message_length", MAX_MSG),
                ("grpc.max_receive_message_length", MAX_MSG),
            ],
        )
        stub = clap_pb2_grpc.CLAPEmbedderStub(channel)

        # -- Health probe (matches Go NewGRPCClient: 4 bytes, 48000Hz) --
        tiny = struct.pack("<f", 0.0)
        resp = stub.GetEmbedding(
            clap_pb2.EmbeddingRequest(pcm_data=tiny, sample_rate=48000)
        )
        assert len(resp.embedding) == 512
        print(f"  Health probe (4 bytes @ 48000Hz): {len(resp.embedding)} dims OK")

        # -- Real IA audio at native sample rate (matches Go analyzeTrack) --
        mp3_data, filename = download_ia_mp3()
        pcm, native_rate = decode_mp3_native(mp3_data)
        pcm_bytes = float32_to_bytes(pcm)
        duration_s = len(pcm) / native_rate
        payload_mb = len(pcm_bytes) / (1024 * 1024)
        print(
            f"  IA audio: {filename} — {duration_s:.1f}s @ {native_rate}Hz, "
            f"{payload_mb:.1f}MB PCM"
        )

        resp2 = stub.GetEmbedding(
            clap_pb2.EmbeddingRequest(pcm_data=pcm_bytes, sample_rate=native_rate),
            timeout=60,
        )
        assert len(resp2.embedding) == 512, f"expected 512, got {len(resp2.embedding)}"
        assert any(v != 0.0 for v in resp2.embedding), "embedding is all zeros"
        norm = np.linalg.norm(resp2.embedding)
        print(
            f"  gRPC @ {native_rate}Hz: {len(resp2.embedding)} dims, "
            f"norm={norm:.4f} OK"
        )

        # -- Same audio forced to 48000Hz (control test) --
        pcm_48k = miniaudio.decode(
            mp3_data,
            output_format=miniaudio.SampleFormat.FLOAT32,
            nchannels=1,
            sample_rate=48000,
        )
        pcm_48k_np = np.frombuffer(pcm_48k.samples, dtype=np.float32)
        resp3 = stub.GetEmbedding(
            clap_pb2.EmbeddingRequest(
                pcm_data=float32_to_bytes(pcm_48k_np), sample_rate=48000
            ),
            timeout=60,
        )
        assert len(resp3.embedding) == 512
        norm_48k = np.linalg.norm(resp3.embedding)
        cosine_sim = np.dot(resp2.embedding, resp3.embedding) / (norm * norm_48k)
        print(
            f"  gRPC @ 48000Hz: {len(resp3.embedding)} dims, "
            f"norm={norm_48k:.4f}, cosine_sim={cosine_sim:.4f} OK"
        )
        assert cosine_sim > 0.9, f"44100/48000 embeddings too different: {cosine_sim}"

        # -- Large payload (30s synthetic @ 44100Hz, ~5.3MB) --
        large = np.random.randn(44100 * 30).astype(np.float32)
        large_mb = len(large.tobytes()) / (1024 * 1024)
        resp4 = stub.GetEmbedding(
            clap_pb2.EmbeddingRequest(
                pcm_data=large.tobytes(), sample_rate=44100
            ),
            timeout=60,
        )
        assert len(resp4.embedding) == 512
        print(f"  Large payload (30s @ 44100Hz, {large_mb:.1f}MB): {len(resp4.embedding)} dims OK")

        channel.close()
    finally:
        proc.terminate()
        proc.wait(timeout=10)


if __name__ == "__main__":
    print("Go-worker-equivalent pipeline test (local CLAP, real IA audio)")
    test_worker_pipeline()
    print("\nAll sidecar tests PASSED")
