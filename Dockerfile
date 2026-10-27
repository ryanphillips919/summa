
FROM golang:1.27 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o summa .

FROM debian:bookworm-slim

WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends python3 python3-venv && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/summa .
COPY --from=builder /app/analytics ./analytics
COPY --from=builder /app/assets ./assets
RUN python3 -m venv /opt/summa-analytics-venv && /opt/summa-analytics-venv/bin/python -m pip install --no-cache-dir -r analytics/requirements.txt
ENV SUMMA_ANALYTICS_PYTHON=/opt/summa-analytics-venv/bin/python

RUN useradd --system --uid 10001 --home-dir /app summa && mkdir -p /app/data && chown summa:summa /app/data
USER summa

EXPOSE 8080

CMD ["./summa"]
