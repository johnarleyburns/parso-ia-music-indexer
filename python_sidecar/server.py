"""
CLAP Inference Sidecar — gRPC server for deep semantic audio embeddings.

Loads the audio encoder from laion/clap-htsat-fused and exposes a gRPC
endpoint for the Go orchestrator to query semantic audio embeddings.

Only the audio encoder + projection layer are retained (~28M params).
The text encoder (~125M params) is discarded to save ~500MB of memory.

Usage:
    pip install -r requirements.txt
    python server.py

The first run downloads ~300MB of model weights. Subsequent runs use the
cached model from the HuggingFace hub cache directory.
"""

import gc

import grpc
from concurrent import futures
import numpy as np
import torch
import torch.nn.functional as F
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

        full_model = ClapModel.from_pretrained(MODEL_NAME)
        self.audio_model = full_model.audio_model
        self.audio_projection = full_model.audio_projection
        self.text_model = full_model.text_model
        self.text_projection = full_model.text_projection
        del full_model
        gc.collect()

        if torch.backends.mps.is_available():
            self.device = torch.device("mps")
        elif torch.cuda.is_available():
            self.device = torch.device("cuda")
        else:
            self.device = torch.device("cpu")

        self.use_fp16 = self.device.type == "mps"
        self.audio_model.to(self.device)
        self.audio_projection.to(self.device)
        self.text_model.to(self.device)
        self.text_projection.to(self.device)
        if self.use_fp16:
            self.audio_model.half()
            self.audio_projection.half()
            self.text_model.half()
            self.text_projection.half()
        self.audio_model.eval()
        self.audio_projection.eval()
        self.text_model.eval()
        self.text_projection.eval()
        dtype_label = "fp16" if self.use_fp16 else "fp32"
        print(f"Audio + text encoders loaded on device: {self.device} ({dtype_label})")

    def GetEmbedding(self, request, context):
        try:
            audio_np = np.frombuffer(request.pcm_data, dtype=np.float32)
            audio_np = resample(audio_np, request.sample_rate, TARGET_SAMPLE_RATE)

            inputs = self.processor(
                audio=audio_np,
                sampling_rate=TARGET_SAMPLE_RATE,
                return_tensors="pt",
            )
            input_features = inputs["input_features"].to(self.device)
            is_longer = inputs.get("is_longer")
            if is_longer is not None:
                is_longer = is_longer.to(self.device)
            if self.use_fp16:
                input_features = input_features.half()

            with torch.no_grad():
                audio_outputs = self.audio_model(
                    input_features=input_features, is_longer=is_longer
                )
                audio_features = self.audio_projection(audio_outputs.pooler_output)
                audio_features = F.normalize(audio_features, dim=-1)

            embedding = audio_features.cpu().numpy().flatten().tolist()
            return clap_pb2.EmbeddingResponse(embedding=embedding)

        except Exception as e:
            print(f"Inference error: {e}")
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return clap_pb2.EmbeddingResponse()

    def GetTextEmbedding(self, request, context):
        try:
            inputs = self.processor(
                text=[request.text],
                return_tensors="pt",
                padding=True,
            )
            input_ids = inputs["input_ids"].to(self.device)
            attention_mask = inputs["attention_mask"].to(self.device)

            with torch.no_grad():
                text_outputs = self.text_model(
                    input_ids=input_ids, attention_mask=attention_mask
                )
                text_features = self.text_projection(text_outputs.pooler_output)
                text_features = F.normalize(text_features, dim=-1)

            embedding = text_features.cpu().numpy().flatten().tolist()
            return clap_pb2.EmbeddingResponse(embedding=embedding)

        except Exception as e:
            print(f"Text inference error: {e}")
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
