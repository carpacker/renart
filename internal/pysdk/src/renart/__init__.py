"""Renart Python SDK.

Available inside renart Python assets. `query()` runs read-only SQL against
the project's connections through the renart runner — credentials never enter
the Python process — and `context` exposes the run window and metadata.

    from renart import query, context

    def materialize():
        games = query("select * from chess_games")
        return games.groupby("winner").size().reset_index(name="wins")
"""

from ._client import (
    BrokerUnavailableError,
    QueryError,
    RenartError,
    query,
)
from .context import context

__all__ = [
    "query",
    "context",
    "RenartError",
    "QueryError",
    "BrokerUnavailableError",
]
