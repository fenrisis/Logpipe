#!/usr/bin/env python3
"""
Kubernetes Log Forwarder for Logpipe

Streams logs from Kubernetes pods and forwards them to Logpipe server.

Usage:
    # Forward all pods in a namespace
    python -m logpipe.k8s_forwarder -n your-namespace

    # Forward specific pods
    python -m logpipe.k8s_forwarder -n your-namespace -p gateway -p worker

    # Forward from multiple namespaces
    python -m logpipe.k8s_forwarder -n your-namespace -n users -n bots

    # Forward all namespaces (careful - lots of logs!)
    python -m logpipe.k8s_forwarder --all-namespaces
"""

import argparse
import asyncio
import json
import re
import socket
import subprocess
import sys
from datetime import datetime, timezone
from typing import Optional


class LogpipeForwarder:
    """Forwards K8s logs to Logpipe server."""

    def __init__(self, host: str = "localhost", port: int = 5555):
        self.host = host
        self.port = port
        self._socket: Optional[socket.socket] = None
        self._lock = asyncio.Lock()

    def _connect(self) -> bool:
        """Connect to Logpipe server."""
        if self._socket is not None:
            return True

        try:
            self._socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            self._socket.settimeout(5.0)
            self._socket.connect((self.host, self.port))
            self._socket.settimeout(None)
            print(f"✓ Connected to Logpipe at {self.host}:{self.port}")
            return True
        except OSError as e:
            print(f"✗ Failed to connect to Logpipe: {e}")
            self._socket = None
            return False

    def send(self, namespace: str, service: str, level: str, message: str, extra: dict = None):
        """Send a log entry to Logpipe."""
        if not self._connect():
            return

        entry = {
            "ts": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            "namespace": namespace,
            "service": service,
            "level": level,
            "message": message,
        }
        if extra:
            entry["extra"] = extra

        try:
            line = json.dumps(entry) + "\n"
            self._socket.sendall(line.encode())
        except OSError:
            self._socket = None

    def close(self):
        """Close connection."""
        if self._socket:
            self._socket.close()
            self._socket = None


def parse_log_level(message: str) -> str:
    """Try to detect log level from message content."""
    # First check for explicit level prefixes (most reliable)
    # Patterns: [INFO], INFO:, INFO , "level":"info", etc.
    level_patterns = [
        r'^\[?(DEBUG|INFO|WARN|WARNING|ERROR|ERR|CRITICAL|FATAL)\]?[:\s]',  # PREFIX: or [PREFIX]
        r'"level"\s*:\s*"?(DEBUG|INFO|WARN|WARNING|ERROR|ERR|CRITICAL|FATAL)"?',  # JSON
        r'\b(DEBUG|INFO|WARN|WARNING|ERROR|ERR|CRITICAL|FATAL)\b.*?[:\-\|]',  # LEVEL - message
    ]

    for pattern in level_patterns:
        match = re.search(pattern, message, re.IGNORECASE)
        if match:
            level = match.group(1).upper()
            if level in ("WARNING",):
                return "WARN"
            if level in ("ERR", "CRITICAL", "FATAL"):
                return "ERROR"
            return level

    # Fallback: check for error indicators (but be more strict)
    msg_lower = message.lower()
    if any(x in msg_lower for x in ["exception", "traceback", "panic:", "fatal:"]):
        return "ERROR"

    return "INFO"


def get_namespaces() -> list[str]:
    """Get list of all namespaces."""
    result = subprocess.run(
        ["kubectl", "get", "namespaces", "-o", "jsonpath={.items[*].metadata.name}"],
        capture_output=True,
        text=True
    )
    if result.returncode != 0:
        print(f"Error getting namespaces: {result.stderr}")
        return []
    return result.stdout.split()


def get_pods(namespace: str) -> list[dict]:
    """Get list of pods in a namespace."""
    result = subprocess.run(
        ["kubectl", "get", "pods", "-n", namespace, "-o", "json"],
        capture_output=True,
        text=True
    )
    if result.returncode != 0:
        print(f"Error getting pods in {namespace}: {result.stderr}")
        return []

    try:
        data = json.loads(result.stdout)
        pods = []
        for item in data.get("items", []):
            pod_name = item["metadata"]["name"]
            # Extract service name from pod name (remove random suffix)
            # e.g., "gateway-7f8b9c6d5-x2j4k" -> "gateway"
            parts = pod_name.rsplit("-", 2)
            if len(parts) >= 2 and len(parts[-1]) <= 5 and len(parts[-2]) <= 10:
                service = parts[0]
            else:
                service = pod_name

            pods.append({
                "name": pod_name,
                "service": service,
                "status": item["status"]["phase"],
            })
        return pods
    except json.JSONDecodeError:
        return []


async def stream_pod_logs(
    forwarder: LogpipeForwarder,
    namespace: str,
    pod_name: str,
    service: str,
    container: Optional[str] = None,
):
    """Stream logs from a single pod."""
    cmd = ["kubectl", "logs", "-f", "--tail=100", "-n", namespace, pod_name]
    if container:
        cmd.extend(["-c", container])

    print(f"  → Streaming {namespace}/{pod_name}")

    process = await asyncio.create_subprocess_exec(
        *cmd,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
    )

    try:
        while True:
            line = await process.stdout.readline()
            if not line:
                break

            message = line.decode("utf-8", errors="replace").strip()
            if not message:
                continue

            level = parse_log_level(message)
            forwarder.send(
                namespace=namespace,
                service=service,
                level=level,
                message=message,
                extra={"pod": pod_name},
            )
    except asyncio.CancelledError:
        process.terminate()
        raise
    except Exception as e:
        print(f"  ✗ Error streaming {pod_name}: {e}")
    finally:
        if process.returncode is None:
            process.terminate()


async def forward_namespace(forwarder: LogpipeForwarder, namespace: str, pod_filter: list[str] = None):
    """Forward logs from all pods in a namespace."""
    pods = get_pods(namespace)
    if not pods:
        print(f"  No pods found in {namespace}")
        return []

    tasks = []
    for pod in pods:
        if pod["status"] != "Running":
            continue

        # Apply pod filter if specified
        if pod_filter:
            if not any(f in pod["name"] or f in pod["service"] for f in pod_filter):
                continue

        task = asyncio.create_task(
            stream_pod_logs(forwarder, namespace, pod["name"], pod["service"])
        )
        tasks.append(task)

    return tasks


async def main():
    parser = argparse.ArgumentParser(
        description="Forward Kubernetes logs to Logpipe",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  %(prog)s -n payments                    # All pods in 'payments' namespace
  %(prog)s -n payments -p gateway         # Only pods matching 'gateway'
  %(prog)s -n payments -n users           # Multiple namespaces
  %(prog)s --all-namespaces               # All namespaces (careful!)
  %(prog)s -n payments --host 10.0.0.1    # Custom Logpipe server
        """
    )
    parser.add_argument("-n", "--namespace", action="append", dest="namespaces",
                        help="Namespace(s) to forward logs from")
    parser.add_argument("-p", "--pod", action="append", dest="pods",
                        help="Filter pods by name (substring match)")
    parser.add_argument("--all-namespaces", action="store_true",
                        help="Forward from all namespaces")
    parser.add_argument("--host", default="localhost",
                        help="Logpipe server host (default: localhost)")
    parser.add_argument("--port", type=int, default=5555,
                        help="Logpipe server port (default: 5555)")
    parser.add_argument("--list", action="store_true",
                        help="List namespaces and pods, then exit")

    args = parser.parse_args()

    # List mode
    if args.list:
        namespaces = args.namespaces or get_namespaces()
        for ns in namespaces:
            print(f"\n{ns}:")
            pods = get_pods(ns)
            for pod in pods:
                status = "●" if pod["status"] == "Running" else "○"
                print(f"  {status} {pod['name']} ({pod['service']})")
        return

    # Determine namespaces
    if args.all_namespaces:
        namespaces = get_namespaces()
    elif args.namespaces:
        namespaces = args.namespaces
    else:
        parser.print_help()
        print("\nError: Specify namespace(s) with -n or use --all-namespaces")
        sys.exit(1)

    print(f"Logpipe K8s Forwarder")
    print(f"{'─' * 40}")
    print(f"Namespaces: {', '.join(namespaces)}")
    if args.pods:
        print(f"Pod filter: {', '.join(args.pods)}")
    print()

    forwarder = LogpipeForwarder(host=args.host, port=args.port)

    # Start forwarding
    all_tasks = []
    for ns in namespaces:
        print(f"[{ns}]")
        tasks = await forward_namespace(forwarder, ns, args.pods)
        all_tasks.extend(tasks)

    if not all_tasks:
        print("\nNo pods to forward. Exiting.")
        return

    print(f"\n✓ Forwarding {len(all_tasks)} pod(s). Press Ctrl+C to stop.\n")

    try:
        await asyncio.gather(*all_tasks)
    except asyncio.CancelledError:
        pass
    except KeyboardInterrupt:
        pass
    finally:
        print("\nStopping...")
        for task in all_tasks:
            task.cancel()
        forwarder.close()


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        print("\nBye!")
