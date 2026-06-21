FROM golang:1.25-alpine AS build
WORKDIR /app
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN go build -o ./bin/go-ledger-system ./cmd/server/main.go

FROM golang:1.25-alpine
WORKDIR /app
COPY --from=build /app/bin/go-ledger-system ./bin/go-ledger-system
COPY --from=build /app/internal/infra/database/repository/domain/migration ./internal/infra/database/repository/domain/migration
COPY --from=build /app/.env .env
CMD ["./bin/go-ledger-system"]