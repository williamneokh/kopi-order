#!/bin/bash

set -e # Exit immediately if a command exits with a non-zero status.

echo "🏪 Kopi Order - Starting Server..."
echo ""
echo "📦 Installing dependencies..."
go mod download

echo ""
echo "🛠️  Building application..."
go build -o kopi-order-server main.go

echo ""
echo "🚀 Starting application on http://localhost:8080"
echo "Press Ctrl+C to stop the server"
echo ""
./kopi-order-server