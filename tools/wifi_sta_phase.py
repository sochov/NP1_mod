"""Host-only, identity-free phase gates for the fixed bounded STA trial."""
import datetime
import json
import os
import pathlib
import re
import uuid


class STAPhaseFiles:
    def __init__(self, root, hold_seconds, off_ready=None, complete=None):
        self.hold_seconds = hold_seconds
        self.off_ready = self._target(root, off_ready)
        self.complete = self._target(root, complete)
        if self.off_ready is not None and self.off_ready == self.complete:
            raise ValueError('STA_PHASE_TARGETS_MUST_DIFFER')
        self.off_confirmed = False

    @staticmethod
    def _target(root, path):
        if path is None:
            return None
        target = pathlib.Path(path).resolve()
        if not target.is_relative_to(pathlib.Path(root).resolve() / 'tmp') or target.exists():
            raise ValueError('STA_PHASE_FRESH_PRIVATE_TARGET_REQUIRED')
        target.parent.mkdir(parents=True, exist_ok=True)
        return target

    @staticmethod
    def _write(path, value):
        if path is None:
            return
        temporary = path.with_name(path.name + '.' + uuid.uuid4().hex + '.pending')
        try:
            with temporary.open('x', encoding='utf8') as stream:
                json.dump(value, stream)
                stream.flush()
                os.fsync(stream.fileno())
            # No-overwrite publication: an unrelated gate is never replaced.
            os.link(temporary, path)
        finally:
            temporary.unlink(missing_ok=True)

    def accept(self, raw):
        match = re.fullmatch(rb'STA_OWNER_OFF_CONFIRMED hold=(3|30|45) utc=(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ)\s*', raw)
        if not match:
            return
        if self.off_confirmed or int(match[1]) != self.hold_seconds:
            raise ValueError('STA_PHASE_OFF_CONFIRMATION_INVALID')
        at = datetime.datetime.strptime(match[2].decode('ascii'), '%Y-%m-%dT%H:%M:%SZ').replace(tzinfo=datetime.timezone.utc)
        deadline = at + datetime.timedelta(seconds=self.hold_seconds)
        if abs((datetime.datetime.now(datetime.timezone.utc) - at).total_seconds()) > 120:
            raise ValueError('STA_PHASE_OFF_TIMESTAMP_UNVERIFIED')
        self._write(self.off_ready, {'offConfirmed': True, 'offAt': at.isoformat(), 'restoreDeadline': deadline.isoformat(), 'holdSeconds': self.hold_seconds})
        self.off_confirmed = True

    def finish(self, restored):
        self._write(self.complete, {'restored': bool(restored), 'offConfirmed': self.off_confirmed,
                                   'completedAt': datetime.datetime.now(datetime.timezone.utc).isoformat()})
