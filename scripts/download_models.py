#!/usr/bin/env python3
import argparse
import os
import sys
from pathlib import Path
from typing import Tuple

from huggingface_hub import snapshot_download

# Model ids
PRETRAINED_TOKENIZER_ID = "NeoQuasar/Kronos-Tokenizer-base"
PRETRAINED_PREDICTOR_ID = "NeoQuasar/Kronos-base"

FINETUNED_1H_TOKENIZER_ID = "lc2004/kronos_tokenizer_base_BTCUSDT_1h_finetune"
FINETUNED_1H_PREDICTOR_ID = "lc2004/kronos_base_model_BTCUSDT_1h_finetune"

FINETUNED_4H_TOKENIZER_ID = "lc2004/kronos_tokenizer_base_BTCUSDT_4h_finetune"
FINETUNED_4H_PREDICTOR_ID = "lc2004/kronos_base_model_BTCUSDT_4h_finetune"

# Default target dirs (relative to repo root)
PRETRAINED_DIR = Path("Kronos/pretrained")
FINETUNED_1H_DIR = Path("BTCUSDT_1h_finetune")
FINETUNED_4H_DIR = Path("BTCUSDT_4h_finetune")


def download(repo_id: str, dest_dir: Path) -> str:
    dest_dir = dest_dir.resolve()
    dest_dir.mkdir(parents=True, exist_ok=True)
    print(f"\n==> Downloading {repo_id} -> {dest_dir}")
    path = snapshot_download(
        repo_id=repo_id,
        local_dir=str(dest_dir),
        local_dir_use_symlinks=False,
        resume_download=True,
    )
    print(f"✓ Done: {path}")
    return path


def download_pretrained() -> Tuple[str, str]:
    tok_path = download(PRETRAINED_TOKENIZER_ID, PRETRAINED_DIR / "Kronos-Tokenizer-base")
    pred_path = download(PRETRAINED_PREDICTOR_ID, PRETRAINED_DIR / "Kronos-base")
    return tok_path, pred_path


def download_finetuned_1h() -> Tuple[str, str]:
    tok_path = download(FINETUNED_1H_TOKENIZER_ID, FINETUNED_1H_DIR / "tokenizer" / "best_model")
    pred_path = download(FINETUNED_1H_PREDICTOR_ID, FINETUNED_1H_DIR / "basemodel" / "best_model")
    return tok_path, pred_path


def download_finetuned_4h() -> Tuple[str, str]:
    tok_path = download(FINETUNED_4H_TOKENIZER_ID, FINETUNED_4H_DIR / "tokenizer" / "best_model")
    pred_path = download(FINETUNED_4H_PREDICTOR_ID, FINETUNED_4H_DIR / "basemodel" / "best_model")
    return tok_path, pred_path


def main():
    parser = argparse.ArgumentParser(description="Download Kronos models from Hugging Face")
    parser.add_argument(
        "target",
        choices=["base", "1h", "4h", "all"],
        nargs="?",
        default="base",
        help="Which models to download: 'base' (pretrained), '1h' (fine-tuned BTCUSDT 1h), '4h', or 'all'",
    )
    args = parser.parse_args()

    try:
        if args.target in ("base", "all"):
            print("Downloading pretrained Kronos models (Tokenizer + Predictor)...")
            download_pretrained()
        if args.target in ("1h", "all"):
            print("Downloading fine-tuned BTCUSDT 1h models (Tokenizer + Predictor)...")
            download_finetuned_1h()
        if args.target in ("4h", "all"):
            print("Downloading fine-tuned BTCUSDT 4h models (Tokenizer + Predictor)...")
            download_finetuned_4h()
    except Exception as e:
        print(f"Error: {e}")
        sys.exit(1)

    print("\nAll requested downloads completed.")


if __name__ == "__main__":
    main()
