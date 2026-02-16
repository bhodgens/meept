"""ClawSkills -- third-party skill management from ClawHub (clawhub.ai).

This module is fully independent from the local skills system
(``meept.skills``).  It provides HTTP client access to the ClawHub
registry, archive validation, installation management, a daemon-side
index, and CLI subcommands.
"""

from meept.clawskills.models import LockFile, LockFileEntry, OriginMetadata

__all__ = [
    "LockFile",
    "LockFileEntry",
    "OriginMetadata",
]
