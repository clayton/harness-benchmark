# Vendored experiment dependencies

## obra/superpowers

Pinned checkout used by the skill-on vs skill-off experiment.

```bash
git clone https://github.com/obra/superpowers.git experiment-deps/superpowers
# pin recorded in configs:
#   skills_version / harness_options.superpowers_sha
cd experiment-deps/superpowers && git rev-parse HEAD
```

Do not edit skills in place for claims; re-pin the SHA in configs when you update.
