FROM node:22.22.0-bookworm-slim@sha256:dd9d21971ec4395903fa6143c2b9267d048ae01ca6d3ea96f16cb30df6187d94

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git \
    && rm -rf /var/lib/apt/lists/* \
    && npm install --global npm@11.6.0 @earendil-works/pi-coding-agent@0.85.1 pi-cursor-sdk@0.3.6

COPY package.json package-lock.json /opt/commander/
RUN cd /opt/commander \
    && npm ci \
    && rm -rf node_modules \
    && npm cache verify

COPY hbench-pi-openrouter hbench-pi-responses hbench-pi-cursor /usr/local/bin/
RUN chmod 0755 /usr/local/bin/hbench-pi-openrouter /usr/local/bin/hbench-pi-responses /usr/local/bin/hbench-pi-cursor

ENV npm_config_offline=true \
    npm_config_audit=false \
    npm_config_fund=false
WORKDIR /workspace
