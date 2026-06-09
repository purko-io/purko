FROM golang:1.25 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o purko-operator ./cmd/operator/

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /app/purko-operator .
USER 65532:65532
ENTRYPOINT ["/purko-operator"]
