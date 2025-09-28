# Expense Tracker

A secure, session-based expense tracking web service written in Go.

## Features

- User registration and login with bcrypt-hashed passwords.
- HttpOnly session cookies with automatic rotation and expiry handling.
- Per-user scoping for expenses, budgets, incomes, recurring expenses, and reports.
- Full CRUD for expenses, budgets, recurring expenses, and incomes.
- Advanced queries for filtering, pagination, and aggregate reporting.

## Getting Started

### Prerequisites

- Go 1.24+

### Installation

1. Clone the repository:
   `sh
   git clone https://your-repository-url/expense-tracker.git
   `
2. Move into the project directory:
   `sh
   cd expense-tracker
   `
3. Download dependencies:
   `sh
   go mod tidy
   `
4. Run the application:
   `sh
   go run main.go
   `

The server listens on port 8080 and persists data to expenses.db in the project root.

### Running Tests

`sh
go test ./...
`

## Authentication

All data endpoints require an authenticated session. The session token is delivered as an HttpOnly cookie named session_token.

### Register

- POST /auth/register
- Request body:
  `json
  {
    "email": "user@example.com",
    "password": "StrongPassword123!"
  }
  `
- Response: 201 Created with the new user payload and a session cookie.

### Login

- POST /auth/login
- Request body matches the register payload.
- Response: 200 OK with the user payload and a refreshed session cookie.

### Logout

- POST /auth/logout
- Clears the session and invalidates the token. Returns 204 No Content.

> Issue register/login requests over HTTPS in production so cookies remain secure (Secure flag is automatically applied for TLS requests).

## API Endpoints

Except for the authentication routes listed above, attach the session_token cookie to every request.

### Expenses

- GET /expenses
  - Query parameters: date_from, date_to, category, mount_min, mount_max, q, limit, offset.
- POST /expenses
  `json
  {
    "amount": 12.34,
    "category": "Food",
    "note": "Lunch with colleagues",
    "date": "2025-09-28T14:30:00Z"
  }
  `
- GET /expenses/{id}
- PUT /expenses/{id}
- DELETE /expenses/{id}

### Aggregates

- GET /expenses/aggregates?query=totals_by_month
- GET /expenses/aggregates?query=totals_by_category

### Budgets

- GET /budgets
- POST /budgets
  `json
  {
    "category": "Food",
    "amount": 500.0,
    "start_date": "2025-09-01T00:00:00Z",
    "end_date": "2025-09-30T23:59:59Z"
  }
  `
- GET /budgets/{id}
- PUT /budgets/{id}
- DELETE /budgets/{id}

### Recurring Expenses

- GET /recurring-expenses
- POST /recurring-expenses
  `json
  {
    "amount": 50.0,
    "category": "Subscription",
    "note": "Streaming Service",
    "frequency": "monthly",
    "next_due_date": "2025-10-01T00:00:00Z"
  }
  `
- GET /recurring-expenses/{id}
- PUT /recurring-expenses/{id}
- DELETE /recurring-expenses/{id}

### Incomes

- GET /incomes
- POST /incomes
  `json
  {
    "amount": 2000.0,
    "source": "Salary",
    "note": "Monthly salary",
    "date": "2025-09-28T10:00:00Z"
  }
  `
- GET /incomes/{id}
- PUT /incomes/{id}
- DELETE /incomes/{id}

### Reports

- GET /reports/income-vs-expense

## Database Schema

All finance tables are scoped to the authenticated user via a foreign key. Existing installations will be upgraded in place.

`sql
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    expires_at DATETIME NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS expenses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    amount REAL NOT NULL,
    category TEXT NOT NULL,
    note TEXT,
    date DATETIME NOT NULL,
    user_id INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS budgets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    category TEXT NOT NULL,
    amount REAL NOT NULL,
    start_date DATETIME NOT NULL,
    end_date DATETIME NOT NULL,
    user_id INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS recurring_expenses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    amount REAL NOT NULL,
    category TEXT NOT NULL,
    note TEXT,
    frequency TEXT NOT NULL,
    next_due_date DATETIME NOT NULL,
    user_id INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS incomes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    amount REAL NOT NULL,
    source TEXT NOT NULL,
    note TEXT,
    date DATETIME NOT NULL,
    user_id INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
`

## Notes

- Session timestamps are stored in UTC and accept RFC3339 payloads.
- Existing finance records without a user association default to user_id = 0; migrate them to real user IDs after enabling auth.
