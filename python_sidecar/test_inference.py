"""
End-to-end test for the CLAP sidecar inference pipeline.

Downloads a real MP3 from Internet Archive, decodes to PCM, and runs
CLAP inference through the locally running gRPC sidecar — matching the
exact pipeline the Go binary uses in production.

All inference runs on the LOCAL machine (model cached in ~/.cache/huggingface/).

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
SAMPLE_RATE = 48000
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


def decode_mp3_to_pcm(mp3_bytes):
    decoded = miniaudio.decode(
        mp3_bytes,
        output_format=miniaudio.SampleFormat.FLOAT32,
        nchannels=1,
        sample_rate=SAMPLE_RATE,
    )
    return np.frombuffer(decoded.samples, dtype=np.float32)


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


def test_local_inference():
    """Load CLAP model locally and run inference on real IA audio."""
    from transformers import AutoProcessor, ClapModel
    import torch

    print("  Loading CLAP model from local cache...")
    model = ClapModel.from_pretrained("laion/clap-htsat-fused")
    processor = AutoProcessor.from_pretrained("laion/clap-htsat-fused")
    model.eval()

    mp3_data, filename = download_ia_mp3()
    pcm = decode_mp3_to_pcm(mp3_data)
    duration_s = len(pcm) / SAMPLE_RATE
    print(f"  Audio: {filename} — {len(mp3_data)} bytes MP3 → {duration_s:.1f}s PCM")

    inputs = processor(audio=pcm, sampling_rate=SAMPLE_RATE, return_tensors="pt")
    with torch.no_grad():
        out = model.get_audio_features(**inputs)
    vec = out.pooler_output.cpu().numpy().flatten().tolist()
    assert len(vec) == 512, f"expected 512 dims, got {len(vec)}"
    assert any(v != 0.0 for v in vec), "embedding is all zeros"
    norm = np.linalg.norm(vec)
    print(f"  Local inference: {len(vec)} dims, norm={norm:.4f} OK")

    tiny = np.array([0.0], dtype=np.float32)
    inputs2 = processor(audio=tiny, sampling_rate=SAMPLE_RATE, return_tensors="pt")
    with torch.no_grad():
        out2 = model.get_audio_features(**inputs2)
    vec2 = out2.pooler_output.cpu().numpy().flatten().tolist()
    assert len(vec2) == 512
    print(f"  Health probe (1 sample): {len(vec2)} dims OK")


def test_grpc_pipeline():
    """Start sidecar locally, send real IA audio via gRPC, verify embedding."""
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

        # 1) Health probe — matches Go NewGRPCClient (4 bytes / 1 float32)
        tiny = struct.pack("<f", 0.0)
        resp = stub.GetEmbedding(
            clap_pb2.EmbeddingRequest(pcm_data=tiny, sample_rate=SAMPLE_RATE)
        )
        assert len(resp.embedding) == 512
        print(f"  Health probe (4 bytes): {len(resp.embedding)} dims OK")

        # 2) Real IA audio — matches Go analyzeTrack pipeline
        mp3_data, filename = download_ia_mp3()
        pcm = decode_mp3_to_pcm(mp3_data)
        pcm_bytes = pcm.tobytes()
        duration_s = len(pcm) / SAMPLE_RATE
        payload_mb = len(pcm_bytes) / (1024 * 1024)
        print(f"  Sending {filename}: {duration_s:.1f}s, {payload_mb:.1f}MB via gRPC...")

        resp2 = stub.GetEmbedding(
            clap_pb2.EmbeddingRequest(pcm_data=pcm_bytes, sample_rate=SAMPLE_RATE),
            timeout=60,
        )
        assert len(resp2.embedding) == 512, f"expected 512, got {len(resp2.embedding)}"
        assert any(v != 0.0 for v in resp2.embedding), "embedding is all zeros"
        norm = np.linalg.norm(resp2.embedding)
        print(f"  gRPC real audio: {len(resp2.embedding)} dims, norm={norm:.4f} OK")

        # 3) Large payload — 30s synthetic audio (~5.5MB, exceeds default 4MB limit)
        large = np.random.randn(SAMPLE_RATE * 30).astype(np.float32)
        large_mb = len(large.tobytes()) / (1024 * 1024)
        resp3 = stub.GetEmbedding(
            clap_pb2.EmbeddingRequest(pcm_data=large.tobytes(), sample_rate=SAMPLE_RATE),
            timeout=60,
        )
        assert len(resp3.embedding) == 512
        print(f"  Large payload ({large_mb:.1f}MB): {len(resp3.embedding)} dims OK")

        channel.close()
    finally:
        proc.terminate()
        proc.wait(timeout=10)


if __name__ == "__main__":
    print("Test 1: Local CLAP inference with real IA audio")
    test_local_inference()
    print("Test 2: gRPC sidecar pipeline with real IA audio")
    test_grpc_pipeline()
    print("\nAll sidecar tests PASSED")
