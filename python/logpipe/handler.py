"""Python logging handler for logpipe."""

import atexit
import json
import logging
import queue
import socket
import threading
from datetime import datetime, timezone
from typing import Any, Optional


class LogpipeHandler(logging.Handler):
    """
    Logging handler that sends logs to logpipe server.

    Usage:
        handler = LogpipeHandler(namespace="myapp", service="api")
        logging.getLogger().addHandler(handler)

    Or use the setup() function for easier configuration.
    """

    def __init__(
        self,
        namespace: str,
        service: str,
        host: str = "localhost",
        port: int = 5555,
        level: int = logging.DEBUG,
        buffer_size: int = 1000,
    ):
        super().__init__(level=level)
        self.namespace = namespace
        self.service = service
        self.host = host
        self.port = port

        self._queue: queue.Queue = queue.Queue(maxsize=buffer_size)
        self._socket: Optional[socket.socket] = None
        self._thread: Optional[threading.Thread] = None
        self._running = False
        self._started = False

    def _ensure_started(self) -> None:
        """Ensure the background sender thread is started."""
        if self._started:
            return

        self._started = True
        self._running = True

        self._thread = threading.Thread(target=self._sender_loop, daemon=True)
        self._thread.start()

        atexit.register(self.close)

    def _connect(self) -> bool:
        """Connect to the logpipe server."""
        if self._socket is not None:
            return True

        try:
            self._socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            self._socket.settimeout(5.0)
            self._socket.connect((self.host, self.port))
            self._socket.settimeout(None)
            return True
        except OSError:
            self._socket = None
            return False

    def _disconnect(self) -> None:
        """Disconnect from the server."""
        if self._socket:
            try:
                self._socket.close()
            except Exception:
                pass
            self._socket = None

    def _sender_loop(self) -> None:
        """Background thread that sends queued logs."""
        while self._running:
            try:
                # Wait for entries with timeout
                try:
                    entry = self._queue.get(timeout=0.5)
                except queue.Empty:
                    continue

                # Try to connect if not connected
                if not self._connect():
                    # Connection failed, put entry back if possible
                    try:
                        self._queue.put_nowait(entry)
                    except queue.Full:
                        pass
                    continue

                # Send the entry
                try:
                    line = json.dumps(entry) + "\n"
                    self._socket.sendall(line.encode())
                except OSError:
                    self._disconnect()
                    try:
                        self._queue.put_nowait(entry)
                    except queue.Full:
                        pass

            except Exception:
                pass  # Don't crash the sender thread

        # Final flush
        self._flush_remaining()

    def _flush_remaining(self) -> None:
        """Flush any remaining entries on shutdown."""
        if not self._connect():
            return

        while not self._queue.empty():
            try:
                entry = self._queue.get_nowait()
                line = json.dumps(entry) + "\n"
                self._socket.sendall(line.encode())
            except Exception:
                break

        self._disconnect()

    def emit(self, record: logging.LogRecord) -> None:
        """Send a log record to the logpipe server."""
        try:
            self._ensure_started()
            entry = self._format_record(record)

            try:
                self._queue.put_nowait(entry)
            except queue.Full:
                pass  # Drop logs if buffer is full
        except Exception:
            self.handleError(record)

    def _format_record(self, record: logging.LogRecord) -> dict[str, Any]:
        """Convert a LogRecord to a logpipe entry."""
        level_map = {
            logging.DEBUG: "DEBUG",
            logging.INFO: "INFO",
            logging.WARNING: "WARN",
            logging.ERROR: "ERROR",
            logging.CRITICAL: "ERROR",
        }

        entry = {
            "ts": datetime.fromtimestamp(record.created, tz=timezone.utc).isoformat(),
            "namespace": self.namespace,
            "service": self.service,
            "level": level_map.get(record.levelno, "INFO"),
            "message": self.format(record) if self.formatter else record.getMessage(),
        }

        # Add extra fields if present
        extra = {}
        standard_attrs = {
            "name", "msg", "args", "created", "filename", "funcName",
            "levelname", "levelno", "lineno", "module", "msecs",
            "pathname", "process", "processName", "relativeCreated",
            "stack_info", "exc_info", "exc_text", "thread", "threadName",
            "message", "asctime", "taskName",
        }

        for key, value in record.__dict__.items():
            if key not in standard_attrs:
                try:
                    json.dumps(value)
                    extra[key] = value
                except (TypeError, ValueError):
                    extra[key] = str(value)

        if extra:
            entry["extra"] = extra

        if hasattr(record, "trace_id"):
            entry["trace_id"] = record.trace_id

        return entry

    def close(self) -> None:
        """Clean up resources."""
        self._running = False

        if self._thread and self._thread.is_alive():
            self._thread.join(timeout=2.0)

        self._disconnect()
        super().close()
