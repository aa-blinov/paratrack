"""Database layer with SQLite."""

import sqlite3
from datetime import datetime
from pathlib import Path

from track.config import DB_PATH
from track.models import Activity, Session, Tag


class Database:
    """SQLite database manager."""

    def __init__(self, db_path: Path | str = DB_PATH):
        self.db_path = db_path
        self.conn: sqlite3.Connection | None = None
        self._init_db()

    def _init_db(self):
        """Initialize database schema."""
        # Convert Path to string for SQLite
        db_str = str(self.db_path) if isinstance(self.db_path, Path) else self.db_path
        # Use check_same_thread=False for web applications
        self.conn = sqlite3.connect(
            db_str, isolation_level=None, check_same_thread=False
        )
        self.conn.row_factory = sqlite3.Row
        self.conn.execute("PRAGMA foreign_keys = ON")

        # Create tables
        self.conn.executescript(
            """
            CREATE TABLE IF NOT EXISTS activities (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL UNIQUE,
                archived BOOLEAN DEFAULT 0,
                created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            );

            CREATE TABLE IF NOT EXISTS sessions (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                activity_id INTEGER NOT NULL,
                start_at TIMESTAMP NOT NULL,
                end_at TIMESTAMP,
                note TEXT,
                paused BOOLEAN DEFAULT 0,
                paused_at TIMESTAMP,
                accumulated_seconds INTEGER DEFAULT 0,
                last_resume_at TIMESTAMP,
                created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                FOREIGN KEY (activity_id) REFERENCES activities(id) ON DELETE CASCADE
            );

            CREATE TABLE IF NOT EXISTS tags (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL UNIQUE,
                created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            );

            CREATE TABLE IF NOT EXISTS session_tags (
                session_id INTEGER NOT NULL,
                tag_id INTEGER NOT NULL,
                PRIMARY KEY (session_id, tag_id),
                FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
                FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
            );

            CREATE TABLE IF NOT EXISTS goals (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                activity_id INTEGER NOT NULL,
                period TEXT NOT NULL CHECK(period IN ('daily', 'weekly', 'monthly')),
                target_minutes INTEGER NOT NULL,
                created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                FOREIGN KEY (activity_id) REFERENCES activities(id) ON DELETE CASCADE
            );

            CREATE TABLE IF NOT EXISTS reminders (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                activity_id INTEGER NOT NULL,
                every_minutes INTEGER NOT NULL,
                window TEXT,
                enabled BOOLEAN DEFAULT 1,
                created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                FOREIGN KEY (activity_id) REFERENCES activities(id) ON DELETE CASCADE
            );

            CREATE INDEX IF NOT EXISTS idx_sessions_activity ON sessions(activity_id);
            CREATE INDEX IF NOT EXISTS idx_sessions_start ON sessions(start_at);
            CREATE INDEX IF NOT EXISTS idx_sessions_end ON sessions(end_at);
            CREATE INDEX IF NOT EXISTS idx_session_tags_session ON session_tags(session_id);
            CREATE INDEX IF NOT EXISTS idx_session_tags_tag ON session_tags(tag_id);
        """
        )

        # Safe migrations for existing DBs: add columns if missing
        self._safe_add_column("sessions", "paused", "BOOLEAN DEFAULT 0")
        self._safe_add_column("sessions", "paused_at", "TIMESTAMP")
        self._safe_add_column("sessions", "accumulated_seconds", "INTEGER DEFAULT 0")
        self._safe_add_column("sessions", "last_resume_at", "TIMESTAMP")

    def close(self):
        """Close database connection."""
        if self.conn:
            self.conn.close()

    def _safe_add_column(self, table: str, column: str, decl: str) -> None:
        """Add a column to a table if it does not already exist."""
        cur = self.conn.execute(f"PRAGMA table_info({table})")
        cols = {row[1] for row in cur.fetchall()}
        if column not in cols:
            self.conn.execute(f"ALTER TABLE {table} ADD COLUMN {column} {decl}")

    def create_activity(self, name: str) -> Activity:
        """Create new activity."""
        cursor = self.conn.execute(
            "INSERT INTO activities (name) VALUES (?) RETURNING *", (name,)
        )
        row = cursor.fetchone()
        return self._row_to_activity(row)

    def get_activity(self, activity_id: int) -> Activity | None:
        """Get activity by ID."""
        cursor = self.conn.execute(
            "SELECT * FROM activities WHERE id = ?", (activity_id,)
        )
        row = cursor.fetchone()
        return self._row_to_activity(row) if row else None

    def get_activity_by_name(self, name: str) -> Activity | None:
        """Get activity by name."""
        cursor = self.conn.execute("SELECT * FROM activities WHERE name = ?", (name,))
        row = cursor.fetchone()
        return self._row_to_activity(row) if row else None

    def get_or_create_activity(self, name: str) -> Activity:
        """Get existing activity or create new one."""
        activity = self.get_activity_by_name(name)
        if activity:
            return activity
        return self.create_activity(name)

    def list_activities(self, archived: bool = False) -> list[Activity]:
        """List all activities."""
        cursor = self.conn.execute(
            "SELECT * FROM activities WHERE archived = ? ORDER BY name", (archived,)
        )
        return [self._row_to_activity(row) for row in cursor.fetchall()]

    # Sessions
    def create_session(
        self,
        activity_id: int,
        start_at: datetime,
        end_at: datetime | None = None,
        note: str | None = None,
    ) -> Session:
        """Create new session."""
        cursor = self.conn.execute(
            """
            INSERT INTO sessions (activity_id, start_at, end_at, note, paused, paused_at, accumulated_seconds, last_resume_at)
            VALUES (?, ?, ?, ?, 0, NULL, 0, ?)
            RETURNING *
            """,
            (activity_id, start_at, end_at, note, start_at),
        )
        row = cursor.fetchone()
        return self._row_to_session(row)

    def get_session(self, session_id: int) -> Session | None:
        """Get session by ID."""
        cursor = self.conn.execute("SELECT * FROM sessions WHERE id = ?", (session_id,))
        row = cursor.fetchone()
        return self._row_to_session(row) if row else None

    def update_session(
        self,
        session_id: int,
        start_at: datetime | None = None,
        end_at: datetime | None = None,
        note: str | None = None,
        activity_id: int | None = None,
        paused: bool | None = None,
        paused_at: datetime | None = None,
        accumulated_seconds: int | None = None,
        last_resume_at: datetime | None = None,
    ) -> Session | None:
        """Update session."""
        updates = []
        params = []

        if start_at is not None:
            updates.append("start_at = ?")
            params.append(start_at)
        if end_at is not None:
            updates.append("end_at = ?")
            params.append(end_at)
        if note is not None:
            updates.append("note = ?")
            params.append(note)
        if activity_id is not None:
            updates.append("activity_id = ?")
            params.append(activity_id)
        if paused is not None:
            updates.append("paused = ?")
            params.append(1 if paused else 0)
        if paused_at is not None:
            updates.append("paused_at = ?")
            params.append(paused_at)
        if accumulated_seconds is not None:
            updates.append("accumulated_seconds = ?")
            params.append(accumulated_seconds)
        if last_resume_at is not None:
            updates.append("last_resume_at = ?")
            params.append(last_resume_at)

        if not updates:
            return self.get_session(session_id)

        updates.append("updated_at = CURRENT_TIMESTAMP")
        params.append(session_id)

        query = f"UPDATE sessions SET {', '.join(updates)} WHERE id = ?"
        self.conn.execute(query, params)
        return self.get_session(session_id)

    def delete_session(self, session_id: int):
        """Delete session."""
        self.conn.execute("DELETE FROM sessions WHERE id = ?", (session_id,))

    def get_active_sessions(self) -> list[tuple[Session, Activity]]:
        """Get all active (not ended) sessions with their activities."""
        cursor = self.conn.execute(
            """
            SELECT s.*, a.name as activity_name
            FROM sessions s
            JOIN activities a ON s.activity_id = a.id
            WHERE s.end_at IS NULL
            ORDER BY s.start_at DESC
            """
        )
        result = []
        for row in cursor.fetchall():
            session = self._row_to_session(row)
            activity = Activity(
                id=row["activity_id"],
                name=row["activity_name"],
                archived=False,
                created_at=None,
                updated_at=None,
            )
            result.append((session, activity))
        return result

    def get_sessions_by_activity(
        self, activity_id: int, limit: int | None = None
    ) -> list[Session]:
        """Get sessions for specific activity."""
        query = "SELECT * FROM sessions WHERE activity_id = ? ORDER BY start_at DESC"
        if limit:
            query += f" LIMIT {limit}"
        cursor = self.conn.execute(query, (activity_id,))
        return [self._row_to_session(row) for row in cursor.fetchall()]

    def get_closed_sessions_overlapping(
        self, start: datetime, end: datetime, activity_id: int | None = None
    ) -> list[tuple[Session, Activity]]:
        """Return closed (ended) sessions overlapping [start, end]."""
        # s.start_at <= end AND s.end_at >= start
        params: list = [end, start]
        activity_filter = ""
        if activity_id is not None:
            activity_filter = " AND s.activity_id = ?"
            params.append(activity_id)
        cursor = self.conn.execute(
            f"""
            SELECT s.id, s.activity_id, s.start_at, s.end_at, s.note,
                   s.paused, s.paused_at, s.accumulated_seconds, s.last_resume_at,
                   s.created_at, s.updated_at, a.name as activity_name
            FROM sessions s
            JOIN activities a ON s.activity_id = a.id
            WHERE s.end_at IS NOT NULL
              AND s.start_at <= ? AND s.end_at >= ?
              {activity_filter}
            ORDER BY s.start_at DESC
            """,
            params,
        )
        result = []
        for row in cursor.fetchall():
            session = self._row_to_session(row)
            activity = Activity(
                id=row["activity_id"],
                name=row["activity_name"],
                archived=False,
                created_at=None,
                updated_at=None,
            )
            result.append((session, activity))
        return result

    # Tags
    def create_tag(self, name: str) -> Tag:
        """Create new tag."""
        cursor = self.conn.execute(
            "INSERT INTO tags (name) VALUES (?) RETURNING *", (name,)
        )
        row = cursor.fetchone()
        return self._row_to_tag(row)

    def get_or_create_tag(self, name: str) -> Tag:
        """Get existing tag or create new one."""
        cursor = self.conn.execute("SELECT * FROM tags WHERE name = ?", (name,))
        row = cursor.fetchone()
        if row:
            return self._row_to_tag(row)
        return self.create_tag(name)

    def add_session_tag(self, session_id: int, tag_id: int):
        """Add tag to session."""
        self.conn.execute(
            "INSERT OR IGNORE INTO session_tags (session_id, tag_id) VALUES (?, ?)",
            (session_id, tag_id),
        )

    def get_session_tags(self, session_id: int) -> list[Tag]:
        """Get all tags for a session."""
        cursor = self.conn.execute(
            """
            SELECT t.* FROM tags t
            JOIN session_tags st ON t.id = st.tag_id
            WHERE st.session_id = ?
            """,
            (session_id,),
        )
        return [self._row_to_tag(row) for row in cursor.fetchall()]

    def list_tags(self) -> list[Tag]:
        """List all tags."""
        cursor = self.conn.execute("SELECT * FROM tags ORDER BY name")
        return [self._row_to_tag(row) for row in cursor.fetchall()]

    # Helper methods
    def _row_to_activity(self, row: sqlite3.Row) -> Activity:
        """Convert database row to Activity."""
        return Activity(
            id=row["id"],
            name=row["name"],
            archived=bool(row["archived"]),
            created_at=(
                datetime.fromisoformat(row["created_at"]) if row["created_at"] else None
            ),
            updated_at=(
                datetime.fromisoformat(row["updated_at"]) if row["updated_at"] else None
            ),
        )

    def _row_to_session(self, row: sqlite3.Row) -> Session:
        """Convert database row to Session."""
        keys = set(row.keys())
        paused_val = row["paused"] if "paused" in keys else 0
        paused_at_val = row["paused_at"] if "paused_at" in keys else None
        acc_val = row["accumulated_seconds"] if "accumulated_seconds" in keys else 0
        last_resume_val = row["last_resume_at"] if "last_resume_at" in keys else None

        return Session(
            id=row["id"],
            activity_id=row["activity_id"],
            start_at=datetime.fromisoformat(row["start_at"]),
            end_at=datetime.fromisoformat(row["end_at"]) if row["end_at"] else None,
            note=row["note"],
            paused=bool(paused_val),
            paused_at=datetime.fromisoformat(paused_at_val) if paused_at_val else None,
            accumulated_seconds=int(acc_val or 0),
            last_resume_at=(
                datetime.fromisoformat(last_resume_val) if last_resume_val else None
            ),
            created_at=(
                datetime.fromisoformat(row["created_at"]) if row["created_at"] else None
            ),
            updated_at=(
                datetime.fromisoformat(row["updated_at"]) if row["updated_at"] else None
            ),
        )

    # Pause/Resume operations
    def pause_session(self, session_id: int) -> Session | None:
        """Pause a running session: accumulate elapsed time and mark paused."""
        session = self.get_session(session_id)
        if not session or session.end_at is not None:
            return session
        now = datetime.now()
        # Elapsed since last_resume_at (fallback to start_at)
        last_start = session.last_resume_at or session.start_at
        elapsed = int((now - last_start).total_seconds())
        new_acc = (session.accumulated_seconds or 0) + max(elapsed, 0)
        return self.update_session(
            session_id,
            paused=True,
            paused_at=now,
            accumulated_seconds=new_acc,
            last_resume_at=None,
        )

    def resume_session(self, session_id: int) -> Session | None:
        """Resume a paused session: clear paused, set last_resume_at."""
        session = self.get_session(session_id)
        if not session or session.end_at is not None:
            return session
        now = datetime.now()
        return self.update_session(
            session_id,
            paused=False,
            paused_at=None,
            last_resume_at=now,
        )

    def _row_to_tag(self, row: sqlite3.Row) -> Tag:
        """Convert database row to Tag."""
        return Tag(
            id=row["id"],
            name=row["name"],
            created_at=(
                datetime.fromisoformat(row["created_at"]) if row["created_at"] else None
            ),
        )


# Global database instance
_db: Database | None = None


def get_db() -> Database:
    """Get global database instance."""
    global _db
    if _db is None:
        _db = Database()
    return _db
