FROM golang:1.16-bullseye AS build

SHELL ["bash", "-Eeuo", "pipefail", "-xc"]

WORKDIR /usr/src/bashbrew

COPY go.mod go.sum ./
RUN go mod download; go mod verify

COPY . .

RUN CGO_ENABLED=0 ./bashbrew.sh --version; \
	cp -al bin/bashbrew /

FROM tianon/docker-tianon

SHELL ["bash", "-Eeuo", "pipefail", "-xc"]

RUN apt-get update; \
	apt-get install -y --no-install-recommends \
		git \
	; \
	rm -rf /var/lib/apt/lists/*

COPY --from=build /bashbrew /usr/local/bin/
RUN bashbrew --version

ENV BASHBREW_CACHE /bashbrew-cache
# make sure our default cache dir exists and is writable by anyone (similar to /tmp)
RUN mkdir -p "$BASHBREW_CACHE"; \
	chmod 1777 "$BASHBREW_CACHE"
# (this allows us to decide at runtime the exact uid/gid we'd like to run as)

VOLUME $BASHBREW_CACHE

COPY scripts/bashbrew-entrypoint.sh /usr/local/bin/
ENTRYPOINT ["bashbrew-entrypoint.sh"]
