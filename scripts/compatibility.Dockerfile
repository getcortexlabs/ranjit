# PostgreSQL tools match the CI server. The compatibility runner uses only the
# Python standard library and exercises released/candidate executables.
FROM postgres:17-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends python3 ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /work
ENTRYPOINT ["python3", "scripts/upgrade_smoke.py"]
