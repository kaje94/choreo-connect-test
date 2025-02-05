
FROM golang:1.23.4-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o go-connect-proxy .


FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/go-connect-proxy .

# Create a user with a known UID/GID within range 10000-20000.
# This is required by Choreo to run the container as a non-root user.
RUN adduser \
    --disabled-password \
    --gecos "" \
    --home "/nonexistent" \
    --shell "/sbin/nologin" \
    --no-create-home \
    --uid 10014 \
    "choreo"
# Use the above created unprivileged user
USER 10014

ENTRYPOINT ["./go-connect-proxy"]
