"""
CLAP Inference Sidecar — gRPC server for deep semantic audio embeddings.

Loads laion/clap-htsat-fused from HuggingFace and exposes a gRPC endpoint
for the Go orchestrator to query semantic audio embeddings.

Usage:
    pip install -r requirements.txt
    python server.py

The first run downloads ~300MB of model weights. Subsequent runs use the
cached model from the HuggingFace hub cache directory.
"""

import grpc
from concurrent import futures
import numpy as np
import torch
from transformers import AutoProcessor, ClapModel

import clap_pb2
import clap_pb2_grpc


MODEL_NAME = "laion/clap-htsat-fused"
TARGET_SAMPLE_RATE = 48000


def resample(audio, src_rate, dst_rate):
    if src_rate == dst_rate:
        return audio
    duration = len(audio) / src_rate
    n_samples = int(duration * dst_rate)
    return np.interp(
        np.linspace(0, len(audio) - 1, n_samples),
        np.arange(len(audio)),
        audio,
    ).astype(np.float32)


class CLAPService(clap_pb2_grpc.CLAPEmbedderServicer):
    def __init__(self):
        print(f"Loading {MODEL_NAME} from Hugging Face...")
        self.processor = AutoProcessor.from_pretrained(MODEL_NAME)
        self.model = ClapModel.from_pretrained(MODEL_NAME)

        if torch.backends.mps.is_available():
            self.device = torch.device("mps")
        elif torch.cuda.is_available():
            self.device = torch.device("cuda")
        else:
            self.device = torch.device("cpu")

        self.model.to(self.device)
        self.model.eval()
        print(f"Model loaded on device: {self.device}")

    def GetEmbedding(self, request, context):
        try:
            audio_np = np.frombuffer(request.pcm_data, dtype=np.float32)
            audio_np = resample(audio_np, request.sample_rate, TARGET_SAMPLE_RATE)

            inputs = self.processor(
                audio=audio_np,
                sampling_rate=TARGET_SAMPLE_RATE,
                return_tensors="pt",
            )
            inputs = {k: v.to(self.device) for k, v in inputs.items()}

            with torch.no_grad():
                audio_outputs = self.model.get_audio_features(**inputs)

            embedding = audio_outputs.pooler_output.cpu().numpy().flatten().tolist()
            return clap_pb2.EmbeddingResponse(embedding=embedding)

        except Exception as e:
            print(f"Inference error: {e}")
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return clap_pb2.EmbeddingResponse()


def serve():
    MAX_MSG = 50 * 1024 * 1024
    server = grpc.server(
        futures.ThreadPoolExecutor(max_workers=4),
        options=[
            ("grpc.max_receive_message_length", MAX_MSG),
            ("grpc.max_send_message_length", MAX_MSG),
        ],
    )
    clap_pb2_grpc.add_CLAPEmbedderServicer_to_server(CLAPService(), server)
    server.add_insecure_port("[::]:50051")
    print("CLAP gRPC server starting on port 50051...")
    server.start()
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
