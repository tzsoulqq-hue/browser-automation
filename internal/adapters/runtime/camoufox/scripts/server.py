import json
import os
import sys

from camoufox.server import launch_server


def main() -> None:
    raw_options = os.environ.get("CAMOUFOX_SERVER_OPTIONS_JSON", "{}")
    options = json.loads(raw_options)
    launch_server(**options)


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(f"camoufox server failed: {exc}", file=sys.stderr, flush=True)
        raise
