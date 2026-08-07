---

### Verify your download

Every artefact is listed in `SHA256SUMS.txt`.

```bash
sha256sum -c SHA256SUMS.txt          # Linux
shasum -a 256 -c SHA256SUMS.txt      # macOS
```

### Signing status

These artefacts are **not code-signed**. On macOS, Gatekeeper will refuse to open the app until
you allow it explicitly (System Settings → Privacy & Security). On Windows, SmartScreen may warn
on first run. Verify the checksum above before allowing either.
