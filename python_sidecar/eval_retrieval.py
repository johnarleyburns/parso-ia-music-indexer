"""
Cross-modal retrieval evaluation harness.

For canonical text prompts, embeds text via the CLAP sidecar's GetTextEmbedding,
cosine-ranks against all stored audio CLAP vectors in the SQLite DB, and prints
top-k results. This is the quality gate that catches embedding regressions.

Usage:
    python eval_retrieval.py --db ../data/parso_indexer.db --top-k 10
"""

import argparse
import json
import os
import sqlite3
import struct
import sys
import time

import grpc
import numpy as np

import clap_pb2
import clap_pb2_grpc

CANONICAL_PROMPTS = [
    "quiet Spanish guitar at dusk",
    "melancholy piano for reading",
    "Gregorian chant in an old cathedral",
    "early jazz from the 1920s",
    "romantic classical guitar",
    "soft public domain music for sleep",
    "baroque strings and harpsichord",
    "nostalgic old recordings",
    "peaceful violin music",
    "dramatic organ music",
    "spanish guitar",
    "gregorian chant",
]

SIDECAR_HOST = os.environ.get("CLAP_HOST", "localhost")
SIDECAR_PORT = int(os.environ.get("CLAP_PORT", "50051"))


def get_text_embedding(stub, text):
    req = clap_pb2.TextEmbeddingRequest(text=text)
    resp = stub.GetTextEmbedding(req)
    return np.array(resp.embedding, dtype=np.float32)


def decode_f16(blob):
    """Decode f16 (float16) blob to float32 numpy array."""
    if len(blob) == 0:
        return np.zeros(512, dtype=np.float32)
    n = len(blob) // 2
    f16 = np.frombuffer(blob, dtype=np.uint16)
    # Convert uint16 f16 to float32
    # f16 layout: 1 sign, 5 exp, 10 mantissa
    sign = (f16 >> 15) & 0x1
    exp = (f16 >> 10) & 0x1F
    mant = f16 & 0x3FF

    sign = sign.astype(np.float32) * -2 + 1
    exp = exp.astype(np.float32)
    mant = mant.astype(np.float32)

    # Normal numbers
    normal = exp > 0
    denormal = exp == 0

    result = np.zeros(n, dtype=np.float32)
    result[normal] = sign[normal] * (1.0 + mant[normal] / 1024.0) * (2.0 ** (exp[normal] - 15.0))
    result[denormal] = sign[denormal] * (mant[denormal] / 1024.0) * (2.0 ** -14.0)

    # Handle infinity/NaN (exp == 31)
    # Just leave as 0 for NaN/inf

    return result


def l2_normalize(v):
    norm = np.linalg.norm(v)
    if norm == 0:
        return v
    return v / norm


def cosine_similarity(a, b):
    return float(np.dot(a, b))


def load_embeddings(db_path):
    """Load all completed track embeddings with metadata."""
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    rows = conn.execute("""
        SELECT e.track_id, e.clap, e.sample_strategy,
               COALESCE(t.title, t.filename) AS track_title,
               t.album_id,
               COALESCE(a.title, a.ia_identifier) AS album_title,
               COALESCE(a.genres, '') AS album_genres,
               COALESCE(t.tags, '') AS track_tags
        FROM track_embeddings e
        INNER JOIN tracks t ON e.track_id = t.id
        INNER JOIN albums a ON t.album_id = a.ia_identifier
        WHERE t.status = 'completed'
        ORDER BY e.track_id
    """)
    embeddings = []
    for r in rows:
        clap_blob = r["clap"]
        vec = decode_f16(clap_blob)
        vec = l2_normalize(vec)
        embeddings.append({
            "track_id": r["track_id"],
            "vector": vec,
            "title": r["track_title"],
            "album_id": r["album_id"],
            "album_title": r["album_title"],
            "genres": r["album_genres"],
            "tags": r["track_tags"],
            "strategy": r["sample_strategy"],
        })
    conn.close()
    return embeddings


def rank_by_cosine(query_vec, embeddings, top_k=10):
    results = []
    for e in embeddings:
        sim = cosine_similarity(query_vec, e["vector"])
        results.append((sim, e))
    results.sort(key=lambda x: x[0], reverse=True)
    return results[:top_k]


def run_eval(db_path, top_k=10, output_json=None):
    print(f"DB: {db_path}")
    print(f"Sidecar: {SIDECAR_HOST}:{SIDECAR_PORT}")
    print(f"Top-K: {top_k}")
    print()

    # Connect to sidecar
    channel = grpc.insecure_channel(f"{SIDECAR_HOST}:{SIDECAR_PORT}")
    stub = clap_pb2_grpc.CLAPEmbedderStub(channel)

    print("Loading track embeddings from DB...")
    t0 = time.time()
    embeddings = load_embeddings(db_path)
    elapsed = time.time() - t0
    print(f"  Loaded {len(embeddings)} tracks in {elapsed:.1f}s")

    if len(embeddings) == 0:
        print("No embeddings found in DB. Index tracks first.")
        channel.close()
        return

    # Strategy breakdown
    strategy_counts = {}
    for e in embeddings:
        s = e.get("strategy", "unknown")
        strategy_counts[s] = strategy_counts.get(s, 0) + 1
    print(f"  Strategy breakdown: {strategy_counts}")
    print()

    all_results = {}

    for prompt in CANONICAL_PROMPTS:
        print(f"--- {prompt!r} ---")
        qv = get_text_embedding(stub, prompt)
        qv = l2_normalize(qv)
        ranked = rank_by_cosine(qv, embeddings, top_k)

        prompt_results = []
        for i, (sim, e) in enumerate(ranked):
            line = f"  {i+1:2d}. cos={sim:.4f}  {e['title']}  [{e['album_title']}]"
            if e["genres"]:
                line += f"  genres: {e['genres']}"
            print(line)
            prompt_results.append({
                "rank": i + 1,
                "cosine": round(sim, 4),
                "track_id": e["track_id"],
                "title": e["title"],
                "album_title": e["album_title"],
                "album_id": e["album_id"],
                "genres": e["genres"],
                "tags": e["tags"],
                "strategy": e["strategy"],
            })
        print()
        all_results[prompt] = prompt_results

    channel.close()

    if output_json:
        with open(output_json, "w") as f:
            json.dump(all_results, f, indent=2)
        print(f"Wrote results to {output_json}")

    return all_results


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Cross-modal retrieval evaluation")
    parser.add_argument("--db", default="../data/parso_indexer.db", help="SQLite database path")
    parser.add_argument("--top-k", type=int, default=10, help="Number of top results to show")
    parser.add_argument("--output-json", default=None, help="Output results as JSON")
    args = parser.parse_args()
    run_eval(args.db, args.top_k, args.output_json)
