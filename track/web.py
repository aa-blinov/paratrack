"""Simple web UI for ParaTrack."""

import csv
from collections import defaultdict
from datetime import datetime, timedelta
from io import StringIO
from pathlib import Path
import sys

from flask import Flask, jsonify, redirect, render_template, request, url_for, Response

# Conditional imports for Gunicorn (Unix-like systems)
if sys.platform != "win32":
    import multiprocessing
    from gunicorn.app.base import BaseApplication
else:
    from waitress import serve

from track.db import get_db

app = Flask(__name__, template_folder=str(Path(__file__).parent / "templates"))
app.secret_key = "dev-secret-key-change-in-production"


# Conditional Gunicorn Application class
if sys.platform != "win32":
    class StandaloneGunicornApplication(BaseApplication):
        def __init__(self, app, options=None):
            self.options = options or {}
            self.application = app
            super().__init__()

        def load_config(self):
            config = {key: value for key, value in self.options.items()
                      if key in self.cfg.settings and value is not None}
            for key, value in config.items():
                self.cfg.set(key.lower(), value)

        def load(self):
            return self.application


def format_duration_seconds(seconds: int) -> str:
    """Format duration in seconds to HH:MM:SS."""
    hours = seconds // 3600
    minutes = (seconds % 3600) // 60
    secs = seconds % 60
    return f"{hours:02d}:{minutes:02d}:{secs:02d}"


def calculate_session_duration(session) -> int:
    """Calculate session duration in seconds."""
    if session.end_at:
        # Closed session
        total_seconds = int((session.end_at - session.start_at).total_seconds())
        return total_seconds

    # Active session
    if session.paused:
        # Return accumulated time only
        return session.accumulated_seconds
    else:
        # Add current running time to accumulated
        now = datetime.now()
        resume_point = session.last_resume_at or session.start_at
        running_seconds = int((now - resume_point).total_seconds())
        return session.accumulated_seconds + running_seconds


def get_period_range(period: str, now: datetime = None):
    """Calculate start and end datetime for a given period.

    Args:
        period: Period name ('today', 'yesterday', 'week', 'month')
        now: Current datetime (defaults to datetime.now())

    Returns:
        Tuple of (start, end, period_label)
    """
    if now is None:
        now = datetime.now()

    if period == "today":
        start = now.replace(hour=0, minute=0, second=0, microsecond=0)
        end = now
        period_label = "Today"
    elif period == "yesterday":
        end = now.replace(hour=0, minute=0, second=0, microsecond=0)
        start = end - timedelta(days=1)
        period_label = "Yesterday"
    elif period == "week":
        start = (now - timedelta(days=now.weekday())).replace(
            hour=0, minute=0, second=0, microsecond=0
        )
        end = now
        period_label = "This Week"
    elif period == "month":
        start = now.replace(day=1, hour=0, minute=0, second=0, microsecond=0)
        end = now
        period_label = "This Month"
    else:
        start = now.replace(hour=0, minute=0, second=0, microsecond=0)
        end = now
        period_label = "Today"

    return start, end, period_label


def clip_session_seconds(session, start: datetime, end: datetime) -> int:
    """Clip session duration to a time window.

    Args:
        session: Session object
        start: Window start time
        end: Window end time

    Returns:
        Clipped duration in seconds
    """
    s_start = max(session.start_at, start)
    s_end = min(session.end_at or end, end)
    return max(0, int((s_end - s_start).total_seconds()))


@app.route("/")
def index():
    """Home page shell."""
    db = get_db()
    activities = db.list_activities(archived=False)
    tags = db.list_tags()
    return render_template("index.html", activities=activities, tags=tags)


def _process_day_for_graph(sessions: list[dict]) -> dict:
    """Process daily sessions to find overlaps and assign lanes."""
    if not sessions:
        return {"lanes": [], "overlaps": [], "total_seconds": 0}

    # Sort sessions by start time
    sessions.sort(key=lambda s: s["start_iso"])

    lanes: list[list[dict]] = []
    for session in sessions:
        placed = False
        session_start = datetime.fromisoformat(session["start_iso"])
        session_end = datetime.fromisoformat(session["end_iso"])

        for lane in lanes:
            can_place = True
            for s_in_lane in lane:
                lane_s_start = datetime.fromisoformat(s_in_lane["start_iso"])
                lane_s_end = datetime.fromisoformat(s_in_lane["end_iso"])
                # Check for overlap
                if session_start < lane_s_end and session_end > lane_s_start:
                    can_place = False
                    break
            if can_place:
                lane.append(session)
                placed = True
                break
        if not placed:
            lanes.append([session])

    overlaps = []
    for i in range(len(sessions)):
        for j in range(i + 1, len(sessions)):
            s1 = sessions[i]
            s2 = sessions[j]
            s1_start = datetime.fromisoformat(s1["start_iso"])
            s1_end = datetime.fromisoformat(s1["end_iso"])
            s2_start = datetime.fromisoformat(s2["start_iso"])
            s2_end = datetime.fromisoformat(s2["end_iso"])

            # Find overlap
            overlap_start = max(s1_start, s2_start)
            overlap_end = min(s1_end, s2_end)

            if overlap_start < overlap_end:
                overlaps.append(
                    {
                        "start_iso": overlap_start.isoformat(),
                        "end_iso": overlap_end.isoformat(),
                        "duration": int((overlap_end - overlap_start).total_seconds()),
                    }
                )
    
    total_seconds = sum(s['duration'] for s in sessions)

    return {"lanes": lanes, "overlaps": overlaps, "total_seconds": total_seconds}


@app.route("/api/data")
def api_data():
    """API endpoint for all app data."""
    db = get_db()

    # --- Active Sessions Data ---
    active_sessions_raw = db.get_active_sessions()
    active_sessions_data = []
    for session, activity in active_sessions_raw:
        duration = calculate_session_duration(session)
        status = "Paused" if session.paused else "Active"
        active_sessions_data.append(
            {
                "id": session.id,
                "activity": activity.name,
                "start_at": session.start_at.strftime("%Y-%m-%d %H:%M"),
                "duration": format_duration_seconds(duration),
                "status": status,
                "note": session.note or "",
            }
        )

    # --- Period-based Data (Stats & Graph) ---
    period = request.args.get("period", "today")
    start, end, period_label = get_period_range(period)
    sessions_raw = db.get_closed_sessions_overlapping(start, end, None)

    # --- Stats Data ---
    stats_sessions_data = []
    total_seconds_stats = 0
    for session, activity in sessions_raw:
        sec = clip_session_seconds(session, start, end)
        if sec <= 0:
            continue
        total_seconds_stats += sec
        stats_sessions_data.append(
            {
                "id": session.id,
                "activity": activity.name,
                "start_at": session.start_at.strftime("%Y-%m-%d %H:%M:%S"),
                "start_at_iso": session.start_at.isoformat(),
                "end_at": (session.end_at or end).strftime("%Y-%m-%d %H:%M:%S"),
                "end_at_iso": (session.end_at or end).isoformat(),
                "duration": format_duration_seconds(sec),
                "duration_seconds": sec,
                "note": session.note or "",
            }
        )

    # --- Graph Data ---
    daily_sessions = defaultdict(list)
    for session, activity in sessions_raw:
        if not session.end_at:
            continue

        s_start = max(session.start_at, start)
        s_end = min(session.end_at, end)
        duration_seconds = int((s_end - s_start).total_seconds())

        if duration_seconds <= 0:
            continue
        
        day_key = s_start.strftime("%Y-%m-%d")
        daily_sessions[day_key].append(
            {
                "name": activity.name,
                "start_iso": s_start.isoformat(),
                "end_iso": s_end.isoformat(),
                "start_hm": s_start.strftime('%H:%M'),
                "end_hm": s_end.strftime('%H:%M'),
                "duration": duration_seconds,
            }
        )

    graph_data = []
    for day_key in sorted(daily_sessions.keys()):
        processed_day = _process_day_for_graph(daily_sessions[day_key])
        graph_data.append(
            {
                "date": day_key,
                "lanes": processed_day["lanes"],
                "overlaps": processed_day["overlaps"],
                "total": format_duration_seconds(processed_day["total_seconds"]),
            }
        )

    return jsonify({
        "active_sessions": active_sessions_data,
        "stats": {
            "sessions": stats_sessions_data,
            "total": format_duration_seconds(total_seconds_stats),
            "period_label": period_label,
            "start": start.strftime("%Y-%m-%d %H:%M"),
            "end": end.strftime("%Y-%m-%d %H:%M"),
        },
        "graph": {
            "data": graph_data,
            "period_label": period_label,
            "start": start.strftime("%Y-%m-%d"),
            "end": end.strftime("%Y-%m-%d"),
        }
    })


@app.route("/api/reports/csv")
def api_reports_csv():
    """API endpoint to generate and download a CSV report of sessions."""
    db = get_db()

    start_date_str = request.args.get("start_date")
    end_date_str = request.args.get("end_date")
    activity_id_str = request.args.get("activity_id")
    tag_ids_str = request.args.get("tag_ids")

    start_date = datetime.fromisoformat(start_date_str) if start_date_str else None
    end_date = datetime.fromisoformat(end_date_str) if end_date_str else None
    activity_id = int(activity_id_str) if activity_id_str else None
    tag_ids = [int(tid) for tid in tag_ids_str.split(',')] if tag_ids_str else None

    sessions_data = db.get_filtered_sessions(start_date, end_date, activity_id, tag_ids)

    # Prepare CSV data
    si = StringIO()
    cw = csv.writer(si)

    headers = [
        "Session ID",
        "Activity",
        "Start Time",
        "End Time",
        "Duration (seconds)",
        "Duration (HH:MM:SS)",
        "Note",
        "Tags",
    ]
    cw.writerow(headers)

    for session, activity, tags in sessions_data:
        duration_seconds = int((session.end_at - session.start_at).total_seconds()) if session.end_at else 0
        tag_names = ", ".join([tag.name for tag in tags])
        cw.writerow(
            [
                session.id,
                activity.name,
                session.start_at.isoformat(),
                session.end_at.isoformat() if session.end_at else "",
                duration_seconds,
                format_duration_seconds(duration_seconds),
                session.note or "",
                tag_names,
            ]
        )

    output = si.getvalue()
    response = Response(output, mimetype="text/csv")
    response.headers["Content-Disposition"] = "attachment; filename=session_report.csv"
    return response


@app.route("/start", methods=["POST"])
def start_activity():
    """Start tracking an activity."""
    db = get_db()
    activity_name = request.form.get("activity", "").strip()
    note = request.form.get("note", "").strip()

    if not activity_name:
        return jsonify({"success": False, "error": "Activity name is required"}), 400

    activity = db.get_or_create_activity(activity_name)
    active = db.get_active_sessions()
    for session, act in active:
        if act.id == activity.id and session.end_at is None:
            return jsonify({"success": False, "error": "Activity already has an active session"}), 409

    db.create_session(
        activity_id=activity.id,
        start_at=datetime.now(),
        note=note if note else None,
    )

    return jsonify({"success": True})


@app.route("/stop/<int:session_id>", methods=["POST"])
def stop_session(session_id):
    """Stop a session."""
    db = get_db()
    db.update_session(session_id, end_at=datetime.now())
    return jsonify({"success": True})


@app.route("/pause/<int:session_id>", methods=["POST"])
def pause_session(session_id):
    """Pause a session."""
    db = get_db()
    db.pause_session(session_id)
    return jsonify({"success": True})


@app.route("/resume/<int:session_id>", methods=["POST"])
def resume_session(session_id):
    """Resume a session."""
    db = get_db()
    db.resume_session(session_id)
    return jsonify({"success": True})


@app.route("/update_session/<int:session_id>", methods=["POST"])
def update_session(session_id):
    """Update session start, end, or duration."""
    db = get_db()
    session = db.get_session(session_id)

    if not session:
        return jsonify({"error": "Session not found"}), 404

    # Get form data
    start_str = request.form.get("start_at")
    end_str = request.form.get("end_at")
    duration_str = request.form.get("duration")
    note = request.form.get("note")

    try:
        update_params = {}
        if note is not None:
            update_params["note"] = note if note.strip() else None

        if start_str and end_str:
            start_dt = datetime.fromisoformat(start_str.replace("Z", ""))
            end_dt = datetime.fromisoformat(end_str.replace("Z", ""))
            if end_dt <= start_dt:
                return jsonify({"error": "End time must be after start time"}), 400
            update_params["start_at"] = start_dt
            update_params["end_at"] = end_dt
            db.update_session(session_id, **update_params)
        elif duration_str:
            parts = duration_str.split(":")
            if len(parts) != 3:
                return jsonify({"error": "Duration must be in HH:MM:SS format"}), 400
            try:
                h, m, s = map(int, parts)
                new_duration = timedelta(hours=h, minutes=m, seconds=s)
                if new_duration.total_seconds() <= 0:
                    return jsonify({"error": "Duration must be positive"}), 400
                
                base_start = session.start_at
                if start_str:
                    base_start = datetime.fromisoformat(start_str.replace("Z", ""))
                    update_params["start_at"] = base_start
                
                update_params["end_at"] = base_start + new_duration
                db.update_session(session_id, **update_params)
            except ValueError:
                return jsonify({"error": "Invalid duration format"}), 400
        elif update_params:
            db.update_session(session_id, **update_params)

        return jsonify({"success": True})
    except Exception as e:
        return jsonify({"error": str(e)}), 400


@app.route("/delete_session/<int:session_id>", methods=["POST"])
def delete_session(session_id):
    """Delete a session."""
    db = get_db()
    try:
        db.delete_session(session_id)
        return jsonify({"success": True})
    except Exception as e:
        return jsonify({"error": str(e)}), 400


def run_server(host="127.0.0.1", port=8000, debug=False):
    """Run the development server (Flask for Windows, Gunicorn for others)."""
    if sys.platform != "win32":
        options = {
            "bind": f"{host}:{port}",
            "workers": (multiprocessing.cpu_count() * 2) + 1,
            "worker_class": "sync",
            "loglevel": "debug" if debug else "info",
            "reload": debug,  # Reload workers on code changes in debug mode
            "timeout": 30,
            "graceful_timeout": 30,
        }
        StandaloneGunicornApplication(app, options).run()
    else:
        # Use Waitress for production on Windows
        print(f"Serving with Waitress on http://{host}:{port}")
        serve(app, host=host, port=port)


if __name__ == "__main__":
    import os
    debug_mode = os.getenv("FLASK_DEBUG", "false").lower() == "true"
    run_server(debug=debug_mode)
