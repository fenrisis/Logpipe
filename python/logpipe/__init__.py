"""
Logpipe - Python SDK for sending logs to logpipe server.

Usage:
    import logpipe
    import logging

    logpipe.setup(
        namespace="payments",
        service="gateway",
        host="localhost",  # or use LOGPIPE_HOST env var
    )

    logger = logging.getLogger(__name__)
    logger.info("Payment processed", extra={"user_id": 123})
"""

import logging
import os
from typing import Optional

from .handler import LogpipeHandler

__version__ = "0.1.0"
__all__ = ["setup", "LogpipeHandler"]

_handler: Optional[LogpipeHandler] = None


def setup(
    namespace: str,
    service: str,
    host: Optional[str] = None,
    port: Optional[int] = None,
    level: int = logging.DEBUG,
    logger_name: Optional[str] = None,
    format: str = "%(message)s",
) -> LogpipeHandler:
    """
    Configure logging to send logs to logpipe server.

    Args:
        namespace: The namespace for your logs (e.g., "payments", "users")
        service: The service name (e.g., "api", "worker", "scheduler")
        host: Logpipe server host (default: LOGPIPE_HOST env or "localhost")
        port: Logpipe server port (default: LOGPIPE_PORT env or 5555)
        level: Minimum log level to capture (default: DEBUG)
        logger_name: Specific logger to configure (default: root logger)
        format: Log message format (default: just the message)

    Returns:
        The LogpipeHandler instance

    Example:
        import logpipe
        import logging

        logpipe.setup(namespace="myapp", service="api")

        logger = logging.getLogger(__name__)
        logger.info("Hello from logpipe!")
    """
    global _handler

    # Get host/port from env vars if not specified
    if host is None:
        host = os.environ.get("LOGPIPE_HOST", "localhost")
    if port is None:
        port = int(os.environ.get("LOGPIPE_PORT", "5555"))

    # Create handler
    _handler = LogpipeHandler(
        namespace=namespace,
        service=service,
        host=host,
        port=port,
        level=level,
    )

    # Set formatter
    _handler.setFormatter(logging.Formatter(format))

    # Add to logger
    logger = logging.getLogger(logger_name)
    logger.addHandler(_handler)

    # Ensure logger level allows the handler to receive messages
    if logger.level == logging.NOTSET or logger.level > level:
        logger.setLevel(level)

    return _handler


def get_handler() -> Optional[LogpipeHandler]:
    """Get the global handler instance."""
    return _handler
