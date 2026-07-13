from typing import Any, Literal

class RenartError(Exception): ...

class BrokerUnavailableError(RenartError): ...

class QueryError(RenartError):
    code: str | None
    def __init__(self, message: str, code: str | None = None) -> None: ...

def query(
    sql: str,
    connection: str | None = None,
    format: Literal["arrow", "pandas"] = "arrow",
) -> Any: ...
