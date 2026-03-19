# Controller — TODO

## Pending

- [ ] **TLS gap** — Gateway references Secrets for TLS, Vrata Listener expects file paths. Need mechanism to mount Secrets as files or extend Vrata to accept inline certs.
- [ ] **Regex overlap detection** — detect semantic overlaps when one of the paths is a RegularExpression. Currently regex paths are skipped by the dedup detector.
