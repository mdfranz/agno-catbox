FROM python:3.12-slim

RUN apt-get update \
 && apt-get install --no-install-recommends -y \
        ca-certificates \
        coreutils \
        findutils \
        gawk \
        grep \
        jq \
        procps \
        sed \
 && rm -rf /var/lib/apt/lists/*

COPY requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir -r /tmp/requirements.txt \
 && rm -f /tmp/requirements.txt

COPY runner.py /runner.py

WORKDIR /workspace
