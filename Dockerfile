FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /vibetype . && mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /vibetype /vibetype
# distroless nonroot runs as uid 65532; a root-owned /data would make SQLite fail to open the DB
COPY --from=build --chown=65532:65532 /data /data
ENV VIBETYPE_DB=/data/vibetype.db
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/vibetype"]
