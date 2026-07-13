"""HTTP client for the renart run broker.

The runner starts a loopback broker per task and injects RENART_API_URL and
RENART_API_TOKEN. Queries execute inside the runner on the project's
connections; results stream back as Arrow IPC. Only the token crosses the
process boundary — never credentials.
"""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.request
from typing import Any, Optional


class RenartError(Exception):
    """Base class for renart SDK errors."""


class BrokerUnavailableError(RenartError):
    """The run broker is not reachable (not running inside a renart run?)."""


class QueryError(RenartError):
    """The broker rejected or failed to execute a query."""

    def __init__(self, message: str, code: Optional[str] = None):
        super().__init__(message)
        self.code = code


def _broker_address() -> "tuple[str, str]":
    url = os.environ.get("RENART_API_URL", "").strip()
    token = os.environ.get("RENART_API_TOKEN", "").strip()
    if not url or not token:
        raise BrokerUnavailableError(
            "renart.query() needs the run broker, which is only available while "
            "the asset runs through renart (RENART_API_URL / RENART_API_TOKEN "
            "are not set)."
        )
    return url, token


def _raise_from_response(exc: urllib.error.HTTPError) -> "None":
    code = None
    message = f"broker request failed with HTTP {exc.code}"
    try:
        payload = json.loads(exc.read().decode("utf-8"))
        error = payload.get("error") or {}
        message = error.get("message") or message
        code = error.get("code")
    except Exception:  # noqa: BLE001 - keep the HTTP error as the message
        pass
    raise QueryError(message, code) from None


def query(sql: str, connection: Optional[str] = None, format: str = "arrow") -> Any:
    """Run a read-only SQL query through the renart runner.

    Args:
        sql: a single SELECT statement. It may reference other assets of the
            project; if a referenced asset is being materialized right now,
            the runner waits for it to finish before executing the query.
        connection: a project connection name; defaults to the asset's own
            connection.
        format: ``"arrow"`` (default) returns a pyarrow.Table,
            ``"pandas"`` returns a pandas.DataFrame.

    Credentials never enter the Python process; the query executes inside
    the renart runner.
    """
    if format not in ("pandas", "arrow"):
        raise ValueError(f'format must be "pandas" or "arrow", got {format!r}')

    url, token = _broker_address()
    body = {"sql": sql}
    if connection:
        body["connection"] = connection
    request = urllib.request.Request(
        url + "/v1/query",
        data=json.dumps(body).encode("utf-8"),
        headers={
            "Authorization": "Bearer " + token,
            "Content-Type": "application/json",
        },
        method="POST",
    )

    import pyarrow.ipc as ipc

    try:
        response = urllib.request.urlopen(request)
    except urllib.error.HTTPError as exc:
        _raise_from_response(exc)
    except urllib.error.URLError as exc:
        raise BrokerUnavailableError(f"cannot reach the run broker: {exc.reason}") from None

    with response:
        table = ipc.open_stream(response).read_all()

    if format == "arrow":
        return table
    try:
        return table.to_pandas()
    except ImportError as exc:
        raise RenartError(
            'query(format="pandas") needs pandas installed in the asset '
            'environment; add pandas to the asset dependencies or use '
            'format="arrow".'
        ) from exc


def _fetch_context() -> "dict[str, Any]":
    """Fetch the run context document from the broker (internal)."""
    url, token = _broker_address()
    request = urllib.request.Request(
        url + "/v1/context",
        headers={"Authorization": "Bearer " + token},
    )
    try:
        with urllib.request.urlopen(request) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        _raise_from_response(exc)
        raise  # unreachable; _raise_from_response always raises
    except urllib.error.URLError as exc:
        raise BrokerUnavailableError(f"cannot reach the run broker: {exc.reason}") from None
