"""Typed access to the run context.

Backed by the BRUIN_* environment variables the runner injects, so it works
in any execution mode (and stays compatible with scripts that read the env
vars directly). Every property returns None when the value is absent, except
``is_full_refresh`` (False) and ``vars`` (``{}``).
"""

from __future__ import annotations

import json
import os
from datetime import date, datetime
from typing import Any, Optional


def _env(name: str) -> Optional[str]:
    value = os.environ.get(name, "").strip()
    return value or None


def _parse_date(value: Optional[str]) -> Optional[date]:
    if not value:
        return None
    try:
        return date.fromisoformat(value)
    except ValueError:
        return None


def _parse_datetime(value: Optional[str]) -> Optional[datetime]:
    if not value:
        return None
    try:
        return datetime.fromisoformat(value)
    except ValueError:
        return None


class _Context:
    """Runtime information about the current renart run."""

    @property
    def start_date(self) -> Optional[date]:
        """Start of the run window."""
        return _parse_date(_env("BRUIN_START_DATE"))

    @property
    def start_datetime(self) -> Optional[datetime]:
        return _parse_datetime(_env("BRUIN_START_DATETIME"))

    @property
    def end_date(self) -> Optional[date]:
        """End of the run window."""
        return _parse_date(_env("BRUIN_END_DATE"))

    @property
    def end_datetime(self) -> Optional[datetime]:
        return _parse_datetime(_env("BRUIN_END_DATETIME"))

    @property
    def execution_date(self) -> Optional[datetime]:
        return _parse_datetime(_env("BRUIN_EXECUTION_DATETIME"))

    @property
    def pipeline(self) -> Optional[str]:
        return _env("BRUIN_PIPELINE")

    @property
    def asset_name(self) -> Optional[str]:
        return _env("BRUIN_ASSET")

    @property
    def connection(self) -> Optional[str]:
        """The asset's default connection name."""
        return _env("BRUIN_CONNECTION")

    @property
    def run_id(self) -> Optional[str]:
        return _env("BRUIN_RUN_ID")

    @property
    def is_full_refresh(self) -> bool:
        value = (_env("BRUIN_FULL_REFRESH") or "").lower()
        return value in ("1", "true", "yes")

    @property
    def vars(self) -> "dict[str, Any]":
        """Pipeline variables, types preserved from their JSON schema."""
        raw = _env("BRUIN_VARS")
        if not raw:
            return {}
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError:
            return {}
        return parsed if isinstance(parsed, dict) else {}

    def __repr__(self) -> str:  # pragma: no cover - debugging nicety
        return (
            f"renart.context(pipeline={self.pipeline!r}, asset={self.asset_name!r}, "
            f"start_date={self.start_date!r}, end_date={self.end_date!r}, "
            f"run_id={self.run_id!r}, is_full_refresh={self.is_full_refresh!r})"
        )


context = _Context()
