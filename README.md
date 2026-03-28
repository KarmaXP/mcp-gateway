# 🚀 MCP Gateway

A high-performance **Model Context Protocol (MCP)** Gateway and Orchestrator built in **Go**.

Specifically engineered for **Platform Engineering** and **SRE** (Site Reliability Engineering) workflows, this gateway provides a centralized, secure, and intelligent entry point for AI agents to interact with distributed infrastructure tools.

## ✨ Key Features
- **Semantic Routing:** Intelligent intent-based request mapping using **Qdrant** vector search.
- **Enterprise-Grade Security:** Unified Authentication and Authorization (JWT/OIDC) across all connected MCP servers.
- **Production Observability:** Full distributed tracing and metrics integration via **OpenTelemetry**.
- **Efficiency:** Built in Go for ultra-low latency (<50ms overhead) and high concurrency.

## 🏗️ Architecture
The system acts as a multiplexer between multiple AI Hosts and multiple MCP Servers. Detailed design documentation is available in the [`docs/`](/docs) directory.

## 🛠️ Getting Started
1. **Prerequisites:** Go 1.26+, Docker & Docker Compose.
2. **Setup:** `go mod download`
3. **Development:** `make run`
4. **Infrastructure:** `make docker-up`

## 📄 License
This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.