"""Self-signed TLS certificate generation utilities.

Uses the ``cryptography`` library to produce an RSA key pair and a
self-signed X.509 certificate suitable for local development or
daemon-to-CLI encrypted communication.
"""

from __future__ import annotations

import datetime
import logging
from pathlib import Path

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.x509.oid import NameOID

logger = logging.getLogger(__name__)

_KEY_SIZE = 4096


def generate_self_signed_cert(
    cert_path: Path,
    key_path: Path,
    hostname: str = "localhost",
    days: int = 365,
) -> None:
    """Generate a self-signed TLS certificate and private key.

    The certificate includes *hostname* as both the Common Name and a
    Subject Alternative Name (DNS entry).  If *hostname* looks like an
    IP address a SAN IP entry is added instead.

    Parameters
    ----------
    cert_path:
        Destination for the PEM-encoded certificate.
    key_path:
        Destination for the PEM-encoded private key.
    hostname:
        The hostname (or IP) to embed in the certificate.
    days:
        Certificate validity period in days.
    """
    # Generate RSA private key.
    key = rsa.generate_private_key(public_exponent=65537, key_size=_KEY_SIZE)

    subject = issuer = x509.Name([
        x509.NameAttribute(NameOID.ORGANIZATION_NAME, "Meept"),
        x509.NameAttribute(NameOID.COMMON_NAME, hostname),
    ])

    now = datetime.datetime.now(datetime.timezone.utc)

    # Build Subject Alternative Names.
    san_entries: list[x509.GeneralName] = [x509.DNSName(hostname)]

    # If hostname looks like an IP, also add an IPAddress SAN.
    try:
        import ipaddress

        ip = ipaddress.ip_address(hostname)
        san_entries.append(x509.IPAddress(ip))
    except ValueError:
        pass  # Not an IP -- DNS-only SAN is fine.

    cert = (
        x509.CertificateBuilder()
        .subject_name(subject)
        .issuer_name(issuer)
        .public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now)
        .not_valid_after(now + datetime.timedelta(days=days))
        .add_extension(
            x509.SubjectAlternativeName(san_entries),
            critical=False,
        )
        .add_extension(
            x509.BasicConstraints(ca=False, path_length=None),
            critical=True,
        )
        .sign(key, hashes.SHA256())
    )

    # Write private key (owner-only permissions).
    key_path.parent.mkdir(parents=True, exist_ok=True)
    key_path.write_bytes(
        key.private_bytes(
            encoding=serialization.Encoding.PEM,
            format=serialization.PrivateFormat.TraditionalOpenSSL,
            encryption_algorithm=serialization.NoEncryption(),
        )
    )
    key_path.chmod(0o600)

    # Write certificate.
    cert_path.parent.mkdir(parents=True, exist_ok=True)
    cert_path.write_bytes(cert.public_bytes(serialization.Encoding.PEM))
    cert_path.chmod(0o644)

    logger.info(
        "Generated self-signed certificate for '%s' (valid %d days): %s",
        hostname,
        days,
        cert_path,
    )


def ensure_certs(
    data_dir: Path,
    hostname: str = "localhost",
    days: int = 365,
) -> tuple[Path, Path]:
    """Return ``(cert_path, key_path)``, generating them if absent.

    Certificates are stored under ``data_dir/tls/``.

    Parameters
    ----------
    data_dir:
        Root data directory (typically ``~/.meept``).
    hostname:
        Hostname embedded in the certificate.
    days:
        Validity period passed to :func:`generate_self_signed_cert`.

    Returns
    -------
    tuple[Path, Path]
        ``(cert_path, key_path)`` pointing to the PEM files.
    """
    tls_dir = data_dir / "tls"
    cert_path = tls_dir / "meept.crt"
    key_path = tls_dir / "meept.key"

    if cert_path.exists() and key_path.exists():
        logger.debug("TLS certificates already exist at %s", tls_dir)
        return cert_path, key_path

    logger.info("TLS certificates not found; generating new pair in %s", tls_dir)
    generate_self_signed_cert(cert_path, key_path, hostname=hostname, days=days)
    return cert_path, key_path
