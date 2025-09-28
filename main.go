package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Expense struct {
	ID       int       `json:"id"`
	Amount   float64   `json:"amount"`
	Category string    `json:"category"`
	Note     string    `json:"note"`
	Date     time.Time `json:"date"`
}

type Budget struct {
	ID        int       `json:"id"`
	Category  string    `json:"category"`
	Amount    float64   `json:"amount"`
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`
}

type RecurringExpense struct {
	ID          int       `json:"id"`
	Amount      float64   `json:"amount"`
	Category    string    `json:"category"`
	Note        string    `json:"note"`
	Frequency   string    `json:"frequency"` // e.g., "daily", "weekly", "monthly"
	NextDueDate time.Time `json:"next_due_date"`
}

type Income struct {
	ID     int       `json:"id"`
	Amount float64   `json:"amount"`
	Source string    `json:"source"`
	Note   string    `json:"note"`
	Date   time.Time `json:"date"`
}

type MonthlyReport struct {
	Month   string  `json:"month"`
	Income  float64 `json:"income"`
	Expense float64 `json:"expense"`
}

var db *sql.DB

func main() {
	var err error
	db, err = sql.Open("sqlite3", "./expenses.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	createTables()

	http.HandleFunc("/expenses", expensesHandler)
	http.HandleFunc("/expenses/", expenseHandler)
	http.HandleFunc("/expenses/aggregates", aggregatesHandler)
	http.HandleFunc("/budgets", budgetsHandler)
	http.HandleFunc("/budgets/", budgetHandler)
	http.HandleFunc("/recurring-expenses", recurringExpensesHandler)
	http.HandleFunc("/recurring-expenses/", recurringExpenseHandler)
	http.HandleFunc("/incomes", incomesHandler)
	http.HandleFunc("/incomes/", incomeHandler)
	http.HandleFunc("/reports/income-vs-expense", incomeVsExpenseReportHandler)

	// Periodically process recurring expenses
	// In a real-world application, this would be a separate, more robust cron job.
	go func() {
		for {
			time.Sleep(24 * time.Hour) // Check once a day
			processRecurringExpenses()
		}
	}()

	log.Println("Server starting on port 8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func createTables() {
	expenseTableStmt := `
	CREATE TABLE IF NOT EXISTS expenses (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		amount REAL NOT NULL,
		category TEXT NOT NULL,
		note TEXT,
		date DATETIME NOT NULL
	);
	`
	_, err := db.Exec(expenseTableStmt)
	if err != nil {
		log.Fatalf("%q: %s", err, expenseTableStmt)
	}

	budgetTableStmt := `
	CREATE TABLE IF NOT EXISTS budgets (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		category TEXT NOT NULL,
		amount REAL NOT NULL,
		start_date DATETIME NOT NULL,
		end_date DATETIME NOT NULL
	);
	`
	_, err = db.Exec(budgetTableStmt)
	if err != nil {
		log.Fatalf("%q: %s", err, budgetTableStmt)
	}

	recurringExpenseTableStmt := `
	CREATE TABLE IF NOT EXISTS recurring_expenses (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		amount REAL NOT NULL,
		category TEXT NOT NULL,
		note TEXT,
		frequency TEXT NOT NULL,
		next_due_date DATETIME NOT NULL
	);
	`
	_, err = db.Exec(recurringExpenseTableStmt)
	if err != nil {
		log.Fatalf("%q: %s", err, recurringExpenseTableStmt)
	}

	incomeTableStmt := `
	CREATE TABLE IF NOT EXISTS incomes (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		amount REAL NOT NULL,
		source TEXT NOT NULL,
		note TEXT,
		date DATETIME NOT NULL
	);
	`
	_, err = db.Exec(incomeTableStmt)
	if err != nil {
		log.Fatalf("%q: %s", err, incomeTableStmt)
	}
}

// ... (existing expense handlers)

func expensesHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		getExpenses(w, r)
	case "POST":
		createExpense(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func expenseHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/expenses/"))
	if err != nil {
		http.Error(w, "Invalid expense ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		getExpense(w, r, id)
	case "PUT":
		updateExpense(w, r, id)
	case "DELETE":
		deleteExpense(w, r, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getExpenses(w http.ResponseWriter, r *http.Request) {
	// Filtering
	query := "SELECT id, amount, category, note, date FROM expenses WHERE 1=1"
	args := []interface{}{}

	if dateFrom := r.URL.Query().Get("date_from"); dateFrom != "" {
		query += " AND date >= ?"
		args = append(args, dateFrom)
	}
	if dateTo := r.URL.Query().Get("date_to"); dateTo != "" {
		query += " AND date <= ?"
		args = append(args, dateTo)
	}
	if category := r.URL.Query().Get("category"); category != "" {
		query += " AND category = ?"
		args = append(args, category)
	}
	if amountMin := r.URL.Query().Get("amount_min"); amountMin != "" {
		query += " AND amount >= ?"
		args = append(args, amountMin)
	}
	if amountMax := r.URL.Query().Get("amount_max"); amountMax != "" {
		query += " AND amount <= ?"
		args = append(args, amountMax)
	}
	if q := r.URL.Query().Get("q"); q != "" {
		query += " AND note LIKE ?"
		args = append(args, "%"+q+"%")
	}

	// Pagination
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 10
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	query += " LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	expenses := []Expense{}
	for rows.Next() {
		var e Expense
		var dateStr string
		if err := rows.Scan(&e.ID, &e.Amount, &e.Category, &e.Note, &dateStr); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		e.Date, _ = time.Parse("2006-01-02 15:04:05", dateStr)
		expenses = append(expenses, e)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(expenses)
}

func createExpense(w http.ResponseWriter, r *http.Request) {
	var e Expense
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	stmt, err := db.Prepare("INSERT INTO expenses(amount, category, note, date) VALUES(?, ?, ?, ?)")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	res, err := stmt.Exec(e.Amount, e.Category, e.Note, e.Date)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := res.LastInsertId()
	e.ID = int(id)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(e)
}

func getExpense(w http.ResponseWriter, r *http.Request, id int) {
	var e Expense
	var dateStr string
	err := db.QueryRow("SELECT id, amount, category, note, date FROM expenses WHERE id = ?", id).Scan(&e.ID, &e.Amount, &e.Category, &e.Note, &dateStr)
	if err == sql.ErrNoRows {
		http.Error(w, "Expense not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	e.Date, _ = time.Parse("2006-01-02 15:04:05", dateStr)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(e)
}

func updateExpense(w http.ResponseWriter, r *http.Request, id int) {
	var e Expense
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	stmt, err := db.Prepare("UPDATE expenses SET amount = ?, category = ?, note = ?, date = ? WHERE id = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	_, err = stmt.Exec(e.Amount, e.Category, e.Note, e.Date, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	e.ID = id
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(e)
}

func deleteExpense(w http.ResponseWriter, r *http.Request, id int) {
	stmt, err := db.Prepare("DELETE FROM expenses WHERE id = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	_, err = stmt.Exec(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func aggregatesHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	switch query {
	case "totals_by_month":
		getTotalsByMonth(w, r)
	case "totals_by_category":
		getTotalsByCategory(w, r)
	default:
		http.Error(w, "Invalid aggregate query", http.StatusBadRequest)
	}
}

func getTotalsByMonth(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT strftime('%Y-%m', date) as month, SUM(amount) as total FROM expenses GROUP BY month ORDER BY month")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	results := map[string]float64{}
	for rows.Next() {
		var month string
		var total float64
		if err := rows.Scan(&month, &total); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		results[month] = total
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func getTotalsByCategory(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT category, SUM(amount) as total FROM expenses GROUP BY category ORDER BY category")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	results := map[string]float64{}
	for rows.Next() {
		var category string
		var total float64
		if err := rows.Scan(&category, &total); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		results[category] = total
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// Budget Handlers

func budgetsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		getBudgets(w, r)
	case "POST":
		createBudget(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func budgetHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/budgets/"))
	if err != nil {
		http.Error(w, "Invalid budget ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		getBudget(w, r, id)
	case "PUT":
		updateBudget(w, r, id)
	case "DELETE":
		deleteBudget(w, r, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getBudgets(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, category, amount, start_date, end_date FROM budgets")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	budgets := []Budget{}
	for rows.Next() {
		var b Budget
		var startDateStr, endDateStr string
		if err := rows.Scan(&b.ID, &b.Category, &b.Amount, &startDateStr, &endDateStr); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		b.StartDate, _ = time.Parse("2006-01-02 15:04:05", startDateStr)
		b.EndDate, _ = time.Parse("2006-01-02 15:04:05", endDateStr)
		budgets = append(budgets, b)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(budgets)
}

func createBudget(w http.ResponseWriter, r *http.Request) {
	var b Budget
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	stmt, err := db.Prepare("INSERT INTO budgets(category, amount, start_date, end_date) VALUES(?, ?, ?, ?)")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	res, err := stmt.Exec(b.Category, b.Amount, b.StartDate, b.EndDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := res.LastInsertId()
	b.ID = int(id)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(b)
}

func getBudget(w http.ResponseWriter, r *http.Request, id int) {
	var b Budget
	var startDateStr, endDateStr string
	err := db.QueryRow("SELECT id, category, amount, start_date, end_date FROM budgets WHERE id = ?", id).Scan(&b.ID, &b.Category, &b.Amount, &startDateStr, &endDateStr)
	if err == sql.ErrNoRows {
		http.Error(w, "Budget not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	b.StartDate, _ = time.Parse("2006-01-02 15:04:05", startDateStr)
	b.EndDate, _ = time.Parse("2006-01-02 15:04:05", endDateStr)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(b)
}

func updateBudget(w http.ResponseWriter, r *http.Request, id int) {
	var b Budget
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	stmt, err := db.Prepare("UPDATE budgets SET category = ?, amount = ?, start_date = ?, end_date = ? WHERE id = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	_, err = stmt.Exec(b.Category, b.Amount, b.StartDate, b.EndDate, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	b.ID = id
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(b)
}

func deleteBudget(w http.ResponseWriter, r *http.Request, id int) {
	stmt, err := db.Prepare("DELETE FROM budgets WHERE id = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	_, err = stmt.Exec(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Recurring Expense Handlers

func recurringExpensesHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		getRecurringExpenses(w, r)
	case "POST":
		createRecurringExpense(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func recurringExpenseHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/recurring-expenses/"))
	if err != nil {
		http.Error(w, "Invalid recurring expense ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		getRecurringExpense(w, r, id)
	case "PUT":
		updateRecurringExpense(w, r, id)
	case "DELETE":
		deleteRecurringExpense(w, r, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getRecurringExpenses(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, amount, category, note, frequency, next_due_date FROM recurring_expenses")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	recurringExpenses := []RecurringExpense{}
	for rows.Next() {
		var re RecurringExpense
		var nextDueDateStr string
		if err := rows.Scan(&re.ID, &re.Amount, &re.Category, &re.Note, &re.Frequency, &nextDueDateStr); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		re.NextDueDate, _ = time.Parse("2006-01-02 15:04:05", nextDueDateStr)
		recurringExpenses = append(recurringExpenses, re)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(recurringExpenses)
}

func createRecurringExpense(w http.ResponseWriter, r *http.Request) {
	var re RecurringExpense
	if err := json.NewDecoder(r.Body).Decode(&re); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	stmt, err := db.Prepare("INSERT INTO recurring_expenses(amount, category, note, frequency, next_due_date) VALUES(?, ?, ?, ?, ?)")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	res, err := stmt.Exec(re.Amount, re.Category, re.Note, re.Frequency, re.NextDueDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := res.LastInsertId()
	re.ID = int(id)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(re)
}

func getRecurringExpense(w http.ResponseWriter, r *http.Request, id int) {
	var re RecurringExpense
	var nextDueDateStr string
	err := db.QueryRow("SELECT id, amount, category, note, frequency, next_due_date FROM recurring_expenses WHERE id = ?", id).Scan(&re.ID, &re.Amount, &re.Category, &re.Note, &re.Frequency, &nextDueDateStr)
	if err == sql.ErrNoRows {
		http.Error(w, "Recurring expense not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	re.NextDueDate, _ = time.Parse("2006-01-02 15:04:05", nextDueDateStr)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(re)
}

func updateRecurringExpense(w http.ResponseWriter, r *http.Request, id int) {
	var re RecurringExpense
	if err := json.NewDecoder(r.Body).Decode(&re); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	stmt, err := db.Prepare("UPDATE recurring_expenses SET amount = ?, category = ?, note = ?, frequency = ?, next_due_date = ? WHERE id = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	_, err = stmt.Exec(re.Amount, re.Category, re.Note, re.Frequency, re.NextDueDate, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	re.ID = id
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(re)
}

func deleteRecurringExpense(w http.ResponseWriter, r *http.Request, id int) {
	stmt, err := db.Prepare("DELETE FROM recurring_expenses WHERE id = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	_, err = stmt.Exec(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// processRecurringExpenses creates expenses for recurring items that are due.
func processRecurringExpenses() {
	log.Println("Processing recurring expenses...")
	rows, err := db.Query("SELECT id, amount, category, note, frequency, next_due_date FROM recurring_expenses WHERE next_due_date <= ?", time.Now())
	if err != nil {
		log.Printf("Error querying recurring expenses: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var re RecurringExpense
		var nextDueDateStr string
		if err := rows.Scan(&re.ID, &re.Amount, &re.Category, &re.Note, &re.Frequency, &nextDueDateStr); err != nil {
			log.Printf("Error scanning recurring expense: %v", err)

			continue
		}
		re.NextDueDate, _ = time.Parse("2006-01-02 15:04:05", nextDueDateStr)

		// Create a new expense
		expense := Expense{
			Amount:   re.Amount,
			Category: re.Category,
			Note:     re.Note,
			Date:     re.NextDueDate,
		}
		_, err := db.Exec("INSERT INTO expenses(amount, category, note, date) VALUES(?, ?, ?, ?)", expense.Amount, expense.Category, expense.Note, expense.Date)
		if err != nil {
			log.Printf("Error creating expense from recurring expense %d: %v", re.ID, err)
			continue
		}

		// Update the next due date
		var nextDueDate time.Time
		switch re.Frequency {
		case "daily":
			nextDueDate = re.NextDueDate.AddDate(0, 0, 1)
		case "weekly":
			nextDueDate = re.NextDueDate.AddDate(0, 0, 7)
		case "monthly":
			nextDueDate = re.NextDueDate.AddDate(0, 1, 0)
		default:
			log.Printf("Invalid frequency for recurring expense %d: %s", re.ID, re.Frequency)
			continue
		}

		_, err = db.Exec("UPDATE recurring_expenses SET next_due_date = ? WHERE id = ?", nextDueDate, re.ID)
		if err != nil {
			log.Printf("Error updating next due date for recurring expense %d: %v", re.ID, err)
		}
	}
}

// Income Handlers

func incomesHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		getIncomes(w, r)
	case "POST":
		createIncome(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func incomeHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/incomes/"))
	if err != nil {
		http.Error(w, "Invalid income ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		getIncome(w, r, id)
	case "PUT":
		updateIncome(w, r, id)
	case "DELETE":
		deleteIncome(w, r, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getIncomes(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, amount, source, note, date FROM incomes")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	incomes := []Income{}
	for rows.Next() {
		var i Income
		var dateStr string
		if err := rows.Scan(&i.ID, &i.Amount, &i.Source, &i.Note, &dateStr); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		i.Date, _ = time.Parse("2006-01-02 15:04:05", dateStr)
		incomes = append(incomes, i)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(incomes)
}

func createIncome(w http.ResponseWriter, r *http.Request) {
	var i Income
	if err := json.NewDecoder(r.Body).Decode(&i); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	stmt, err := db.Prepare("INSERT INTO incomes(amount, source, note, date) VALUES(?, ?, ?, ?)")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	res, err := stmt.Exec(i.Amount, i.Source, i.Note, i.Date)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := res.LastInsertId()
	i.ID = int(id)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(i)
}

func getIncome(w http.ResponseWriter, r *http.Request, id int) {
	var i Income
	var dateStr string
	err := db.QueryRow("SELECT id, amount, source, note, date FROM incomes WHERE id = ?", id).Scan(&i.ID, &i.Amount, &i.Source, &i.Note, &dateStr)
	if err == sql.ErrNoRows {
		http.Error(w, "Income not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	i.Date, _ = time.Parse("2006-01-02 15:04:05", dateStr)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(i)
}

func updateIncome(w http.ResponseWriter, r *http.Request, id int) {
	var i Income
	if err := json.NewDecoder(r.Body).Decode(&i); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	stmt, err := db.Prepare("UPDATE incomes SET amount = ?, source = ?, note = ?, date = ? WHERE id = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	_, err = stmt.Exec(i.Amount, i.Source, i.Note, i.Date, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	i.ID = id
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(i)
}

func deleteIncome(w http.ResponseWriter, r *http.Request, id int) {
	stmt, err := db.Prepare("DELETE FROM incomes WHERE id = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	_, err = stmt.Exec(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func incomeVsExpenseReportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	incomeRows, err := db.Query("SELECT strftime('%Y-%m', date) as month, SUM(amount) as total FROM incomes GROUP BY month")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer incomeRows.Close()

	reports := make(map[string]*MonthlyReport)

	for incomeRows.Next() {
		var month string
		var total float64
		if err := incomeRows.Scan(&month, &total); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		reports[month] = &MonthlyReport{Month: month, Income: total}
	}

	expenseRows, err := db.Query("SELECT strftime('%Y-%m', date) as month, SUM(amount) as total FROM expenses GROUP BY month")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer expenseRows.Close()

	for expenseRows.Next() {
		var month string
		var total float64
		if err := expenseRows.Scan(&month, &total); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if report, ok := reports[month]; ok {
			report.Expense = total
		} else {
			reports[month] = &MonthlyReport{Month: month, Expense: total}
		}
	}

	// Convert map to slice for consistent JSON output order
	var result []MonthlyReport
	for _, report := range reports {
		result = append(result, *report)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
