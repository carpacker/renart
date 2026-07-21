from typing import Literal, overload

from pandas import DataFrame
from pyarrow import Table

class RenartError(Exception): ...

class BrokerUnavailableError(RenartError): ...

class QueryError(RenartError):
    code: str | None
    def __init__(self, message: str, code: str | None = None) -> None: ...

@overload
def query(
    sql: str,
    connection: str | None = None,
    format: Literal["arrow"] = "arrow",
) -> Table: ...

@overload
def query(
    sql: str,
    connection: str | None = None,
    *,
    format: Literal["pandas"],
) -> DataFrame: ...

@overload
def query(
    sql: str,
    connection: str | None,
    format: Literal["pandas"],
) -> DataFrame: ...
