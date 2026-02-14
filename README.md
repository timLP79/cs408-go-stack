# Go Full-Stack Web Application

A simple full-stack web application built with Go, Gin web framework, and SQLite database.

**CS408 Spring 2026 Project** | [GitHub Issues](https://github.com/timLP79/cs408-go-stack/issues) | [Project Board](https://github.com/timLP79/cs408-go-stack/projects)

## Tech Stack

- **Go 1.24+** with [Gin](https://github.com/gin-gonic/gin) web framework
- **SQLite** via [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) (pure Go, no CGo)
- **Go `html/template`** with layout pattern
- **Bootstrap 5** (CDN)

## Quick Start

```bash
git clone https://github.com/timLP79/cs408-go-stack.git
cd cs408-go-stack
go mod download
go run .
```

Visit `http://localhost:3000` in your browser.

## Documentation

Full project documentation is in the [`docs/`](./docs/) folder, including:

- Project status, sprint plan, and learning notes
- [Technical implementation plan](./docs/plan.md)
- [Deployment guide (EC2 + systemd)](./docs/week6/deployment.md)
- [Go learning guide](./docs/tutorials/GO_LEARNING_GUIDE.md)
- [Bootstrap integration guide](./docs/week3/BOOTSTRAP_INTEGRATION_GUIDE.md)
- [Testing and debugging guide](./docs/week3/TESTING_AND_DEBUGGING_GUIDE.md)
- [Tech stack survey](./docs/week3/tech-stack-survey.md)

## License

(To be determined)
