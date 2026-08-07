from __future__ import annotations

import json
import sqlite3
import time
import uuid
from pathlib import Path
from typing import Any, Dict, Iterable, List, Mapping, Optional

from edge_agent.types import Message


class SessionStore:
    def __init__(self, path: str) -> None:
        target = Path(path).expanduser().resolve()
        target.parent.mkdir(parents=True, exist_ok=True)
        self.path = target
        self.connection = sqlite3.connect(str(target))
        self.connection.row_factory = sqlite3.Row
        self._initialize()

    def _initialize(self) -> None:
        self.connection.executescript(
            """
            PRAGMA journal_mode=WAL;
            CREATE TABLE IF NOT EXISTS sessions (
                id TEXT PRIMARY KEY,
                created_at REAL NOT NULL,
                updated_at REAL NOT NULL,
                metadata_json TEXT NOT NULL
            );
            CREATE TABLE IF NOT EXISTS messages (
                session_id TEXT NOT NULL,
                seq INTEGER NOT NULL,
                message_json TEXT NOT NULL,
                created_at REAL NOT NULL,
                PRIMARY KEY (session_id, seq),
                FOREIGN KEY (session_id) REFERENCES sessions(id)
            );
            CREATE TABLE IF NOT EXISTS events (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                session_id TEXT NOT NULL,
                event_type TEXT NOT NULL,
                payload_json TEXT NOT NULL,
                created_at REAL NOT NULL
            );
            """
        )
        self.connection.commit()

    def ensure_session(
        self, session_id: Optional[str] = None, metadata: Optional[Mapping[str, Any]] = None
    ) -> str:
        value = session_id or uuid.uuid4().hex
        now = time.time()
        self.connection.execute(
            "INSERT OR IGNORE INTO sessions(id, created_at, updated_at, metadata_json) "
            "VALUES (?, ?, ?, ?)",
            (value, now, now, json.dumps(dict(metadata or {}), ensure_ascii=False)),
        )
        self.connection.commit()
        return value

    def append_message(self, session_id: str, message: Message) -> int:
        self.ensure_session(session_id)
        row = self.connection.execute(
            "SELECT COALESCE(MAX(seq), -1) + 1 AS next_seq FROM messages WHERE session_id = ?",
            (session_id,),
        ).fetchone()
        seq = int(row["next_seq"])
        now = time.time()
        self.connection.execute(
            "INSERT INTO messages(session_id, seq, message_json, created_at) VALUES (?, ?, ?, ?)",
            (
                session_id,
                seq,
                json.dumps(message.to_dict(), ensure_ascii=False),
                now,
            ),
        )
        self.connection.execute(
            "UPDATE sessions SET updated_at = ? WHERE id = ?", (now, session_id)
        )
        self.connection.commit()
        return seq

    def load_messages(self, session_id: str) -> List[Message]:
        rows = self.connection.execute(
            "SELECT message_json FROM messages WHERE session_id = ? ORDER BY seq",
            (session_id,),
        ).fetchall()
        return [Message.from_dict(json.loads(row["message_json"])) for row in rows]

    def append_event(
        self, session_id: str, event_type: str, payload: Mapping[str, Any]
    ) -> None:
        self.ensure_session(session_id)
        self.connection.execute(
            "INSERT INTO events(session_id, event_type, payload_json, created_at) "
            "VALUES (?, ?, ?, ?)",
            (
                session_id,
                event_type,
                json.dumps(dict(payload), ensure_ascii=False, default=str),
                time.time(),
            ),
        )
        self.connection.commit()

    def export_trajectory(
        self,
        session_id: str,
        tools: Optional[Iterable[Mapping[str, Any]]] = None,
        metadata: Optional[Mapping[str, Any]] = None,
    ) -> Dict[str, Any]:
        value: Dict[str, Any] = {
            "session_id": session_id,
            "messages": [message.to_dict() for message in self.load_messages(session_id)],
        }
        if tools is not None:
            value["tools"] = list(tools)
        if metadata:
            value["metadata"] = dict(metadata)
        return value

    def close(self) -> None:
        self.connection.close()

    def __enter__(self) -> "SessionStore":
        return self

    def __exit__(self, *args: object) -> None:
        self.close()
