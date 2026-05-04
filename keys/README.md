# Publisher signing key

Aileron's install pipeline (ADR-0004) verifies every connector and
action download against the publisher's ed25519 public key. To trust
this publisher, users add the contents of `publisher.pub` (committed
once it's available) to their `~/.aileron/keyring.json`.

## Generating the keypair (publisher one-time setup)

```sh
# Generate a fresh ed25519 keypair.
openssl genpkey -algorithm ed25519 -out publisher.key
openssl pkey -in publisher.key -pubout -out publisher.pub

# Encode the private key for the GitHub Actions secret.
base64 -i publisher.key | pbcopy

# In GitHub: Settings → Secrets and variables → Actions → New repository
# secret. Name: AILERON_SIGNING_KEY. Value: paste from clipboard.

# Store publisher.key out-of-repo (1Password, etc.) and DELETE the local
# file once the secret is set:
rm publisher.key

# Commit publisher.pub:
git add publisher.pub
git commit -m "feat(keys): add publisher signing key (public)"
```

## Trusting this publisher (consumer side)

```sh
# Append to ~/.aileron/keyring.json — see ADR-0004 for the schema.
mkdir -p ~/.aileron
# (manual edit; tool support coming in #404 — see Aileron Phase 7)
```

The placeholder content of `publisher.pub` is empty until the keypair
is generated; until that's done, the install pipeline fails closed for
this publisher.
