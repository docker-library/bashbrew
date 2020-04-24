FROM tianon/docker-tianon

SHELL ["bash", "-Eeuo", "pipefail", "-xc"]

RUN apt-get update; \
	apt-get install -y --no-install-recommends \
		golang-go \
	; \
	rm -rf /var/lib/apt/lists/*

WORKDIR /usr/src/bashbrew
COPY go.mod go.sum ./
COPY cmd cmd
COPY vendor vendor
RUN export CGO_ENABLED=0; \
	go build -mod vendor -v -o /usr/local/bin/bashbrew ./cmd/bashbrew; \
	rm -r ~/.cache/go-build; \
	bashbrew --help > /dev/null

ENV BASHBREW_CACHE /bashbrew-cache
# make sure our default cache dir exists and is writable by anyone (similar to /tmp)
RUN mkdir -p "$BASHBREW_CACHE"; \
	chmod 1777 "$BASHBREW_CACHE"
# (this allows us to decide at runtime the exact uid/gid we'd like to run as)

VOLUME $BASHBREW_CACHE

COPY scripts/bashbrew-entrypoint.sh /usr/local/bin/
ENTRYPOINT ["bashbrew-entrypoint.sh"]
