# Expense Tracker

This is a simple expense tracker web service built with Go.

## Features

*   Create, Read, Update, and Delete expenses.
*   Filter expenses by date, category, amount, and keywords.
*   Paginate results.
*   Get aggregate expense data by month and category.
*   Create, Read, Update, and Delete budgets.
*   Create, Read, Update, and Delete recurring expenses.
*   Create, Read, Update, and Delete incomes.

## Getting Started

### Prerequisites

*   Go 1.x

### Installation

1.  Clone the repository:
    ```sh
    git clone https://your-repository-url/expense-tracker.git
    ```
2.  Navigate to the project directory:
    ```sh
    cd expense-tracker
    ```
3.  Install dependencies:
    ```sh
    go mod tidy
    ```
4.  Run the application:
    ```sh
    go run main.go
    ```
The server will start on port 8080.

## API Endpoints

### Expenses

*   `GET /expenses`: Get a list of expenses.
    *   **Query Parameters:**
        *   `date_from` (YYYY-MM-DD): Filter by start date.
        *   `date_to` (YYYY-MM-DD): Filter by end date.
        *   `category`: Filter by category.
        *   `amount_min`: Filter by minimum amount.
        *   `amount_max`: Filter by maximum amount.
        *   `q`: Search for a keyword in the note.
        *   `limit`: Number of items per page (default: 10).
        *   `offset`: Page offset (default: 0).
*   `POST /expenses`: Create a new expense.
    *   **Request Body:**
        ```json
        {
            "amount": 12.34,
            "category": "Food",
            "note": "Lunch with colleagues",
            "date": "2025-09-28T14:30:00Z"
        }
        ```
*   `GET /expenses/{id}`: Get a single expense by ID.
*   `PUT /expenses/{id}`: Update an expense by ID.
*   `DELETE /expenses/{id}`: Delete an expense by ID.

### Aggregates

*   `GET /expenses/aggregates?query=totals_by_month`: Get total expenses by month.
*   `GET /expenses/aggregates?query=totals_by_category`: Get total expenses by category.

### Budgets

*   `GET /budgets`: Get a list of budgets.
*   `POST /budgets`: Create a new budget.
    *   **Request Body:**
        ```json
        {
            "category": "Food",
            "amount": 500.00,
            "start_date": "2025-09-01T00:00:00Z",
            "end_date": "2025-09-30T23:59:59Z"
        }
        ```
*   `GET /budgets/{id}`: Get a single budget by ID.
*   `PUT /budgets/{id}`: Update a budget by ID.
*   `DELETE /budgets/{id}`: Delete a budget by ID.

### Recurring Expenses

*   `GET /recurring-expenses`: Get a list of recurring expenses.
*   `POST /recurring-expenses`: Create a new recurring expense.
    *   **Request Body:**
        ```json
        {
            "amount": 50.00,
            "category": "Subscription",
            "note": "Streaming Service",
            "frequency": "monthly",
            "next_due_date": "2025-10-01T00:00:00Z"
        }
        ```
*   `GET /recurring-expenses/{id}`: Get a single recurring expense by ID.
*   `PUT /recurring-expenses/{id}`: Update a recurring expense by ID.
*   `DELETE /recurring-expenses/{id}`: Delete a recurring expense by ID.

### Incomes

*   `GET /incomes`: Get a list of incomes.
*   `POST /incomes`: Create a new income.
    *   **Request Body:**
        ```json
        {
            "amount": 2000.00,
            "source": "Salary",
            "note": "Monthly salary",
            "date": "2025-09-28T10:00:00Z"
        }
        ```
*   `GET /incomes/{id}`: Get a single income by ID.
*   `PUT /incomes/{id}`: Update an income by ID.
*   `DELETE /incomes/{id}`: Delete an income by ID.

### Reports

*   `GET /reports/income-vs-expense`: Get a monthly report of income vs. expenses.

## Database Schema

The application uses a SQLite database (`expenses.db`) with the following schema:

```sql
CREATE TABLE IF NOT EXISTS expenses (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    amount REAL NOT NULL,
    category TEXT NOT NULL,
    note TEXT,
    date DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS budgets (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    category TEXT NOT NULL,
    amount REAL NOT NULL,
    start_date DATETIME NOT NULL,
    end_date DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS recurring_expenses (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    amount REAL NOT NULL,
    category TEXT NOT NULL,
    note TEXT,
    frequency TEXT NOT NULL,
    next_due_date DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS incomes (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    amount REAL NOT NULL,
    source TEXT NOT NULL,
    note TEXT,
    date DATETIME NOT NULL
);
```
