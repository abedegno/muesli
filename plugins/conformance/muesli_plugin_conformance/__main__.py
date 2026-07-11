import argparse
import sys

import httpx

from .runner import run_conformance


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="muesli_plugin_conformance",
        description="Validate a Muesli plugin endpoint against the contract.",
    )
    parser.add_argument("url", help="Base URL of the plugin (e.g. http://localhost:8000)")
    parser.add_argument("--kind", required=True, choices=["transcriber", "agent"])
    parser.add_argument("--token", required=True, help="Per-plugin shared bearer token")
    parser.add_argument("--timeout", type=float, default=300.0)
    args = parser.parse_args(argv)

    with httpx.Client(base_url=args.url.rstrip("/"), timeout=args.timeout) as client:
        report = run_conformance(client, kind=args.kind, token=args.token)

    print(report.summary())
    print(f"\n{'CONFORMANT' if report.ok else 'NON-CONFORMANT'}")
    return 0 if report.ok else 1


if __name__ == "__main__":
    sys.exit(main())
