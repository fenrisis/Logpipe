"""Async TCP client for sending logs to logpipe server."""

import asyncio
import json
import logging
from collections import deque
from datetime import datetime
from typing import Any, Optional

logger = logging.getLogger(__name__)


class LogpipeClient:
    """Async TCP client with buffering for reliable log delivery."""

    def __init__(
        self,
        host: str = "localhost",
        port: int = 5555,
        buffer_size: int = 1000,
        flush_interval: float = 0.1,
        connect_timeout: float = 5.0,
        reconnect_delay: float = 1.0,
    ):
        self.host = host
        self.port = port
        self.buffer_size = buffer_size
        self.flush_interval = flush_interval
        self.connect_timeout = connect_timeout
        self.reconnect_delay = reconnect_delay

        self._buffer: deque = deque(maxlen=buffer_size)
        self._writer: Optional[asyncio.StreamWriter] = None
        self._reader: Optional[asyncio.StreamReader] = None
        self._running = False
        self._flush_task: Optional[asyncio.Task] = None
        self._lock = asyncio.Lock()

    async def start(self) -> None:
        """Start the client and background flush loop."""
        if self._running:
            return

        self._running = True
        await self._connect()
        self._flush_task = asyncio.create_task(self._flush_loop())

    async def stop(self) -> None:
        """Stop the client and flush remaining logs."""
        self._running = False

        if self._flush_task:
            self._flush_task.cancel()
            try:
                await self._flush_task
            except asyncio.CancelledError:
                pass

        # Final flush
        await self._flush()
        await self._disconnect()

    async def send(self, entry: dict[str, Any]) -> None:
        """Queue a log entry for sending."""
        # Ensure timestamp is serializable
        if "ts" in entry and isinstance(entry["ts"], datetime):
            entry["ts"] = entry["ts"].isoformat()

        self._buffer.append(entry)

    async def _connect(self) -> bool:
        """Establish connection to the server."""
        if self._writer is not None:
            return True

        try:
            self._reader, self._writer = await asyncio.wait_for(
                asyncio.open_connection(self.host, self.port),
                timeout=self.connect_timeout,
            )
            logger.debug(f"Connected to logpipe server at {self.host}:{self.port}")
            return True
        except (OSError, asyncio.TimeoutError) as e:
            logger.warning(f"Failed to connect to logpipe server: {e}")
            return False

    async def _disconnect(self) -> None:
        """Close the connection."""
        if self._writer:
            try:
                self._writer.close()
                await self._writer.wait_closed()
            except Exception:
                pass
            self._writer = None
            self._reader = None

    async def _flush(self) -> None:
        """Send all buffered logs to the server."""
        if not self._buffer:
            return

        async with self._lock:
            if self._writer is None:
                if not await self._connect():
                    return

            entries_to_send = []
            while self._buffer:
                entries_to_send.append(self._buffer.popleft())

            try:
                for entry in entries_to_send:
                    line = json.dumps(entry) + "\n"
                    self._writer.write(line.encode())
                await self._writer.drain()
            except Exception as e:
                logger.warning(f"Failed to send logs: {e}")
                # Put entries back in buffer (may lose some if buffer is full)
                for entry in reversed(entries_to_send):
                    if len(self._buffer) < self.buffer_size:
                        self._buffer.appendleft(entry)
                await self._disconnect()

    async def _flush_loop(self) -> None:
        """Background loop that periodically flushes the buffer."""
        while self._running:
            try:
                await asyncio.sleep(self.flush_interval)
                await self._flush()
            except asyncio.CancelledError:
                break
            except Exception as e:
                logger.error(f"Flush loop error: {e}")
                await asyncio.sleep(self.reconnect_delay)


# Singleton client instance
_client: Optional[LogpipeClient] = None


def get_client() -> Optional[LogpipeClient]:
    """Get the global client instance."""
    return _client


def set_client(client: LogpipeClient) -> None:
    """Set the global client instance."""
    global _client
    _client = client
