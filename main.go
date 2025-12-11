package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

type Expense struct {
	ID        int       `json:"id"`
	Amount    float64   `json:"amount"`
	Category  string    `json:"category"`
	Note      string    `json:"note"`
	Date      time.Time `json:"date"`
	AccountID *int      `json:"account_id"` // Optional
	UserID    int       `json:"-"`
}

type Budget struct {
	ID        int       `json:"id"`
	Category  string    `json:"category"`
	Amount    float64   `json:"amount"`
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`
	UserID    int       `json:"-"`
}

type RecurringExpense struct {
	ID          int       `json:"id"`
	Amount      float64   `json:"amount"`
	Category    string    `json:"category"`
	Note        string    `json:"note"`
	Frequency   string    `json:"frequency"`
	NextDueDate time.Time `json:"next_due_date"`
	UserID      int       `json:"-"`
}

type Income struct {
	ID        int       `json:"id"`
	Amount    float64   `json:"amount"`
	Source    string    `json:"source"`
	Note      string    `json:"note"`
	Date      time.Time `json:"date"`
	AccountID *int      `json:"account_id"` // Optional for backward compatibility/flexibility
	UserID    int       `json:"-"`
}

type Account struct {
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	Type    string  `json:"type"` // e.g., "Cash", "Bank", "E-Wallet"
	Balance float64 `json:"balance"`
	UserID  int     `json:"-"`
}

type MonthlyReport struct {
	Month   string  `json:"month"`
	Income  float64 `json:"income"`
	Expense float64 `json:"expense"`
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	ID    int    `json:"id"`
	Email string `json:"email"`
}

const (
	sessionCookieName   = "session_token"
	sessionTTL          = 24 * time.Hour
	sessionRefreshDelta = sessionTTL / 3
	timeFormat          = "2006-01-02 15:04:05"
	maxJSONBody         = 1 << 20
	bcryptCost          = 12
)

var db *sql.DB

func main() {
	var err error
	db, err = sql.Open("sqlite3", "./expenses.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		log.Fatalf("failed to enable foreign keys: %v", err)
	}

	if err := createTables(); err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}

	if err := ensureAccountColumns(); err != nil {
		log.Fatalf("failed to migrate database (accounts): %v", err)
	}

	http.HandleFunc("/auth/register", registerHandler)
	http.HandleFunc("/auth/login", loginHandler)
	http.HandleFunc("/auth/logout", logoutHandler)

	http.HandleFunc("/expenses", withAuth(expensesHandler))
	http.HandleFunc("/expenses/", withAuth(expenseHandler))
	http.HandleFunc("/expenses/aggregates", withAuth(aggregatesHandler))
	http.HandleFunc("/budgets", withAuth(budgetsHandler))
	http.HandleFunc("/budgets/", withAuth(budgetHandler))
	http.HandleFunc("/recurring-expenses", withAuth(recurringExpensesHandler))
	http.HandleFunc("/recurring-expenses/", withAuth(recurringExpenseHandler))
	http.HandleFunc("/incomes", withAuth(incomesHandler))
	http.HandleFunc("/incomes/", withAuth(incomeHandler))
	http.HandleFunc("/reports/income-vs-expense", withAuth(incomeVsExpenseReportHandler))
	http.HandleFunc("/accounts", withAuth(accountsHandler))
	http.HandleFunc("/accounts/", withAuth(accountHandler))

	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			processRecurringExpenses()
		}
	}()

	log.Println("Server starting on port 8090...")
	log.Fatal(http.ListenAndServe(":8090", nil))
}
func createTables() error {
	userTableStmt := `
    CREATE TABLE IF NOT EXISTS users (
        id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
        email TEXT NOT NULL UNIQUE,
        password_hash TEXT NOT NULL,
        created_at DATETIME NOT NULL
    );
    `
	if _, err := db.Exec(userTableStmt); err != nil {
		return fmt.Errorf("create users table: %w", err)
	}

	accountTableStmt := `
    CREATE TABLE IF NOT EXISTS accounts (
        id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL,
        type TEXT NOT NULL,
        balance REAL NOT NULL DEFAULT 0,
        user_id INTEGER NOT NULL,
        FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
    );
    `
	if _, err := db.Exec(accountTableStmt); err != nil {
		return fmt.Errorf("create accounts table: %w", err)
	}

	sessionTableStmt := `
    CREATE TABLE IF NOT EXISTS sessions (
        token_hash TEXT NOT NULL PRIMARY KEY,
        user_id INTEGER NOT NULL,
        expires_at DATETIME NOT NULL,
        FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
    );
    `
	if _, err := db.Exec(sessionTableStmt); err != nil {
		return fmt.Errorf("create sessions table: %w", err)
	}

	expenseTableStmt := `
    CREATE TABLE IF NOT EXISTS expenses (
        id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
        amount REAL NOT NULL,
        category TEXT NOT NULL,
        note TEXT,
        date DATETIME NOT NULL,
        user_id INTEGER NOT NULL,
        FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
    );
    `
	if _, err := db.Exec(expenseTableStmt); err != nil {
		return fmt.Errorf("create expenses table: %w", err)
	}

	budgetTableStmt := `
    CREATE TABLE IF NOT EXISTS budgets (
        id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
        category TEXT NOT NULL,
        amount REAL NOT NULL,
        start_date DATETIME NOT NULL,
        end_date DATETIME NOT NULL,
        user_id INTEGER NOT NULL,
        FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
    );
    `
	if _, err := db.Exec(budgetTableStmt); err != nil {
		return fmt.Errorf("create budgets table: %w", err)
	}

	recurringExpenseTableStmt := `
    CREATE TABLE IF NOT EXISTS recurring_expenses (
        id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
        amount REAL NOT NULL,
        category TEXT NOT NULL,
        note TEXT,
        frequency TEXT NOT NULL,
        next_due_date DATETIME NOT NULL,
        user_id INTEGER NOT NULL,
        FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
    );
    `
	if _, err := db.Exec(recurringExpenseTableStmt); err != nil {
		return fmt.Errorf("create recurring_expenses table: %w", err)
	}

	incomeTableStmt := `
    CREATE TABLE IF NOT EXISTS incomes (
        id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
        amount REAL NOT NULL,
        source TEXT NOT NULL,
        note TEXT,
        date DATETIME NOT NULL,
        user_id INTEGER NOT NULL,
        FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
    );
    `
	if _, err := db.Exec(incomeTableStmt); err != nil {
		return fmt.Errorf("create incomes table: %w", err)
	}

	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id)"); err != nil {
		return fmt.Errorf("create sessions index: %w", err)
	}

	tables := []string{"expenses", "budgets", "recurring_expenses", "incomes"}
	for _, table := range tables {
		if err := ensureUserScopedTable(table); err != nil {
			return err
		}
	}

	return nil
}

func ensureAccountColumns() error {
	tables := []string{"expenses", "incomes"}
	for _, table := range tables {
		rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
		if err != nil {
			return fmt.Errorf("inspect %s schema: %w", table, err)
		}

		hasAccountID := false
		for rows.Next() {
			var cid int
			var name, ctype string
			var notNull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
				rows.Close()
				return fmt.Errorf("scan %s schema: %w", table, err)
			}
			if strings.EqualFold(name, "account_id") {
				hasAccountID = true
			}
		}
		rows.Close()

		if !hasAccountID {
			alter := fmt.Sprintf("ALTER TABLE %s ADD COLUMN account_id INTEGER REFERENCES accounts(id) ON DELETE SET NULL", table)
			if _, err := db.Exec(alter); err != nil {
				return fmt.Errorf("add account_id to %s: %w", table, err)
			}
		}
	}
	return nil
}

func ensureUserScopedTable(table string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return fmt.Errorf("inspect %s schema: %w", table, err)
	}
	defer rows.Close()

	hasUserID := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan %s schema: %w", table, err)
		}
		if strings.EqualFold(name, "user_id") {
			hasUserID = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s schema: %w", table, err)
	}

	if !hasUserID {
		alter := fmt.Sprintf("ALTER TABLE %s ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0", table)
		if _, err := db.Exec(alter); err != nil {
			return fmt.Errorf("add user_id to %s: %w", table, err)
		}
	}

	indexStmt := fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_user ON %s(user_id)", table, table)
	if _, err := db.Exec(indexStmt); err != nil {
		return fmt.Errorf("create %s user index: %w", table, err)
	}

	return nil
}
func registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var creds credentials
	if !decodeJSONBody(w, r, &creds) {
		return
	}

	email, err := sanitizeEmail(creds.Email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := validatePassword(creds.Password); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(creds.Password), bcryptCost)
	if err != nil {
		log.Printf("password hashing error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	createdAt := time.Now().UTC().Format(timeFormat)
	res, err := db.Exec("INSERT INTO users(email, password_hash, created_at) VALUES(?, ?, ?)", email, string(passwordHash), createdAt)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			http.Error(w, "Email already registered", http.StatusConflict)
			return
		}
		log.Printf("user insert error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	id, err := res.LastInsertId()
	if err != nil {
		log.Printf("user id fetch error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := issueSession(w, r, int(id)); err != nil {
		log.Printf("issue session error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(authResponse{ID: int(id), Email: email})
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var creds credentials
	if !decodeJSONBody(w, r, &creds) {
		return
	}

	email, err := sanitizeEmail(creds.Email)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	if strings.TrimSpace(creds.Password) == "" {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	var userID int
	var passwordHash string
	err = db.QueryRow("SELECT id, password_hash FROM users WHERE email = ?", email).Scan(&userID, &passwordHash)
	if err == sql.ErrNoRows {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	} else if err != nil {
		log.Printf("user lookup error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(creds.Password)); err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	if err := issueSession(w, r, userID); err != nil {
		log.Printf("issue session error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(authResponse{ID: userID, Email: email})
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		tokenHash := hashSessionToken(cookie.Value)
		if _, err := db.Exec("DELETE FROM sessions WHERE token_hash = ?", tokenHash); err != nil {
			log.Printf("session delete error: %v", err)
		}
	}

	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

type authedHandler func(http.ResponseWriter, *http.Request, int)

func withAuth(handler authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authenticateAndRefreshSession(w, r)
		if !ok {
			return
		}
		handler(w, r, userID)
	}
}

func authenticateAndRefreshSession(w http.ResponseWriter, r *http.Request) (int, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return 0, false
	}

	tokenHash := hashSessionToken(cookie.Value)
	var userID int
	var expiresAtStr string
	err = db.QueryRow("SELECT user_id, expires_at FROM sessions WHERE token_hash = ?", tokenHash).Scan(&userID, &expiresAtStr)
	if err == sql.ErrNoRows {
		clearSessionCookie(w)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return 0, false
	} else if err != nil {
		log.Printf("session lookup error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return 0, false
	}

	expiresAt, err := parseTimestamp(expiresAtStr)
	if err != nil {
		log.Printf("session expiry parse error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return 0, false
	}

	now := time.Now().UTC()
	if now.After(expiresAt) {
		_, _ = db.Exec("DELETE FROM sessions WHERE token_hash = ?", tokenHash)
		clearSessionCookie(w)
		http.Error(w, "Session expired", http.StatusUnauthorized)
		return 0, false
	}

	if expiresAt.Sub(now) < sessionRefreshDelta {
		newExpiry := now.Add(sessionTTL)
		if _, err := db.Exec("UPDATE sessions SET expires_at = ? WHERE token_hash = ?", newExpiry.Format(timeFormat), tokenHash); err != nil {
			log.Printf("session refresh error: %v", err)
		} else {
			setSessionCookie(w, r, cookie.Value, newExpiry)
		}
	}

	return userID, true
}

func issueSession(w http.ResponseWriter, r *http.Request, userID int) error {
	rawToken, tokenHash, err := generateSessionToken()
	if err != nil {
		return err
	}

	expiresAt := time.Now().UTC().Add(sessionTTL)

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	if _, err := tx.Exec("DELETE FROM sessions WHERE user_id = ?", userID); err != nil {
		tx.Rollback()
		return err
	}

	if _, err := tx.Exec("INSERT INTO sessions(token_hash, user_id, expires_at) VALUES(?, ?, ?)", tokenHash, userID, expiresAt.Format(timeFormat)); err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	setSessionCookie(w, r, rawToken, expiresAt)
	return nil
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r != nil && r.TLS != nil,
	}
	if cookie.MaxAge < 0 {
		cookie.MaxAge = 0
	}
	http.SetCookie(w, cookie)
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func generateSessionToken() (string, string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)
	return raw, hashSessionToken(raw), nil
}

func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			http.Error(w, fmt.Sprintf("Invalid JSON at byte %d", syntaxErr.Offset), http.StatusBadRequest)
			return false
		}
		if errors.Is(err, io.EOF) {
			http.Error(w, "Request body must not be empty", http.StatusBadRequest)
			return false
		}
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return false
	}

	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "Request body must only contain a single JSON object", http.StatusBadRequest)
		return false
	}

	return true
}

func sanitizeEmail(email string) (string, error) {
	trimmed := strings.TrimSpace(strings.ToLower(email))
	if trimmed == "" {
		return "", errors.New("Email is required")
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil || parsed.Address == "" {
		return "", errors.New("Invalid email address")
	}
	return strings.ToLower(parsed.Address), nil
}

func validatePassword(password string) error {
	if strings.TrimSpace(password) == "" {
		return errors.New("Password is required")
	}
	length := utf8.RuneCountInString(password)
	if length < 12 {
		return errors.New("Password must be at least 12 characters")
	}
	if length > 128 {
		return errors.New("Password must be 128 characters or fewer")
	}
	return nil
}

func isValidFrequency(freq string) bool {
	switch strings.ToLower(strings.TrimSpace(freq)) {
	case "daily", "weekly", "monthly", "yearly":
		return true
	default:
		return false
	}
}
func expensesHandler(w http.ResponseWriter, r *http.Request, userID int) {
	switch r.Method {
	case http.MethodGet:
		getExpenses(w, r, userID)
	case http.MethodPost:
		createExpense(w, r, userID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func expenseHandler(w http.ResponseWriter, r *http.Request, userID int) {
	idStr := strings.TrimPrefix(r.URL.Path, "/expenses/")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid expense ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		getExpense(w, r, userID, id)
	case http.MethodPut:
		updateExpense(w, r, userID, id)
	case http.MethodDelete:
		deleteExpense(w, r, userID, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getExpenses(w http.ResponseWriter, r *http.Request, userID int) {
	query := "SELECT id, amount, category, note, date FROM expenses WHERE user_id = ?"
	args := []interface{}{userID}

	params := r.URL.Query()

	if dateFrom := strings.TrimSpace(params.Get("date_from")); dateFrom != "" {
		query += " AND date >= ?"
		args = append(args, dateFrom)
	}
	if dateTo := strings.TrimSpace(params.Get("date_to")); dateTo != "" {
		query += " AND date <= ?"
		args = append(args, dateTo)
	}
	if category := strings.TrimSpace(params.Get("category")); category != "" {
		query += " AND category = ?"
		args = append(args, category)
	}
	if amountMin := strings.TrimSpace(params.Get("amount_min")); amountMin != "" {
		query += " AND amount >= ?"
		args = append(args, amountMin)
	}
	if amountMax := strings.TrimSpace(params.Get("amount_max")); amountMax != "" {
		query += " AND amount <= ?"
		args = append(args, amountMax)
	}
	if q := strings.TrimSpace(params.Get("q")); q != "" {
		query += " AND note LIKE ?"
		args = append(args, "%"+q+"%")
	}

	limit, err := strconv.Atoi(params.Get("limit"))
	if err != nil || limit <= 0 {
		limit = 10
	} else if limit > 100 {
		limit = 100
	}
	offset, err := strconv.Atoi(params.Get("offset"))
	if err != nil || offset < 0 {
		offset = 0
	}

	query += " LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var expenses []Expense
	for rows.Next() {
		var e Expense
		var dateStr string
		if err := rows.Scan(&e.ID, &e.Amount, &e.Category, &e.Note, &dateStr); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		parsedDate, err := parseTimestamp(dateStr)
		if err != nil {
			log.Printf("expense date parse error: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		e.Date = parsedDate
		e.UserID = userID
		expenses = append(expenses, e)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(expenses)
}

func createExpense(w http.ResponseWriter, r *http.Request, userID int) {
	var e Expense
	if !decodeJSONBody(w, r, &e) {
		return
	}

	if e.Date.IsZero() {
		e.Date = time.Now().UTC()
	} else {
		e.Date = e.Date.UTC()
	}

	if e.AccountID == nil || *e.AccountID == 0 {
		http.Error(w, "Account is required", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		log.Printf("tx begin error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	stmt, err := tx.Prepare("INSERT INTO expenses(amount, category, note, date, user_id, account_id) VALUES(?, ?, ?, ?, ?, ?)")
	if err != nil {
		tx.Rollback()
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	res, err := stmt.Exec(e.Amount, e.Category, e.Note, e.Date.Format(timeFormat), userID, e.AccountID)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	id, err := res.LastInsertId()
	if err != nil {
		tx.Rollback()
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Update Account Balance if linked
	if e.AccountID != nil {
		_, err := tx.Exec("UPDATE accounts SET balance = balance - ? WHERE id = ? AND user_id = ?", e.Amount, *e.AccountID, userID)
		if err != nil {
			tx.Rollback()
			log.Printf("failed to update account balance: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("tx commit error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	e.ID = int(id)
	e.UserID = userID

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(e)
}

func getExpense(w http.ResponseWriter, r *http.Request, userID, id int) {
	var e Expense
	var dateStr string
	err := db.QueryRow("SELECT id, amount, category, note, date FROM expenses WHERE id = ? AND user_id = ?", id, userID).Scan(&e.ID, &e.Amount, &e.Category, &e.Note, &dateStr)
	if err == sql.ErrNoRows {
		http.Error(w, "Expense not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	parsedDate, err := parseTimestamp(dateStr)
	if err != nil {
		log.Printf("expense date parse error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	e.Date = parsedDate
	e.UserID = userID

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(e)
}

func updateExpense(w http.ResponseWriter, r *http.Request, userID, id int) {
	var e Expense
	if !decodeJSONBody(w, r, &e) {
		return
	}

	if e.Date.IsZero() {
		e.Date = time.Now().UTC()
	} else {
		e.Date = e.Date.UTC()
	}

	stmt, err := db.Prepare("UPDATE expenses SET amount = ?, category = ?, note = ?, date = ? WHERE id = ? AND user_id = ?")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	res, err := stmt.Exec(e.Amount, e.Category, e.Note, e.Date.Format(timeFormat), id, userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "Expense not found", http.StatusNotFound)
		return
	}

	e.ID = id
	e.UserID = userID

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(e)
}

func deleteExpense(w http.ResponseWriter, r *http.Request, userID, id int) {
	res, err := db.Exec("DELETE FROM expenses WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "Expense not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
func aggregatesHandler(w http.ResponseWriter, r *http.Request, userID int) {
	switch r.URL.Query().Get("query") {
	case "totals_by_month":
		getTotalsByMonth(w, userID)
	case "totals_by_category":
		getTotalsByCategory(w, userID)
	default:
		http.Error(w, "Invalid aggregate query", http.StatusBadRequest)
	}
}

func getTotalsByMonth(w http.ResponseWriter, userID int) {
	rows, err := db.Query("SELECT strftime('%Y-%m', date) AS month, SUM(amount) AS total FROM expenses WHERE user_id = ? GROUP BY month ORDER BY month", userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	results := map[string]float64{}
	for rows.Next() {
		var month string
		var total float64
		if err := rows.Scan(&month, &total); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		results[month] = total
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func getTotalsByCategory(w http.ResponseWriter, userID int) {
	rows, err := db.Query("SELECT category, SUM(amount) AS total FROM expenses WHERE user_id = ? GROUP BY category ORDER BY category", userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	results := map[string]float64{}
	for rows.Next() {
		var category string
		var total float64
		if err := rows.Scan(&category, &total); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		results[category] = total
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func budgetsHandler(w http.ResponseWriter, r *http.Request, userID int) {
	switch r.Method {
	case http.MethodGet:
		getBudgets(w, userID)
	case http.MethodPost:
		createBudget(w, r, userID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func budgetHandler(w http.ResponseWriter, r *http.Request, userID int) {
	idStr := strings.TrimPrefix(r.URL.Path, "/budgets/")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid budget ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		getBudget(w, userID, id)
	case http.MethodPut:
		updateBudget(w, r, userID, id)
	case http.MethodDelete:
		deleteBudget(w, userID, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getBudgets(w http.ResponseWriter, userID int) {
	rows, err := db.Query("SELECT id, category, amount, start_date, end_date FROM budgets WHERE user_id = ? ORDER BY start_date", userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var budgets []Budget
	for rows.Next() {
		var b Budget
		var startStr, endStr string
		if err := rows.Scan(&b.ID, &b.Category, &b.Amount, &startStr, &endStr); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		startDate, err := parseTimestamp(startStr)
		if err != nil {
			log.Printf("budget start date parse error: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		endDate, err := parseTimestamp(endStr)
		if err != nil {
			log.Printf("budget end date parse error: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		b.StartDate = startDate
		b.EndDate = endDate
		b.UserID = userID
		budgets = append(budgets, b)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(budgets)
}

func createBudget(w http.ResponseWriter, r *http.Request, userID int) {
	var b Budget
	if !decodeJSONBody(w, r, &b) {
		return
	}

	if b.StartDate.IsZero() {
		b.StartDate = time.Now().UTC()
	} else {
		b.StartDate = b.StartDate.UTC()
	}
	if b.EndDate.IsZero() {
		b.EndDate = b.StartDate
	} else {
		b.EndDate = b.EndDate.UTC()
	}

	stmt, err := db.Prepare("INSERT INTO budgets(category, amount, start_date, end_date, user_id) VALUES(?, ?, ?, ?, ?)")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	res, err := stmt.Exec(b.Category, b.Amount, b.StartDate.Format(timeFormat), b.EndDate.Format(timeFormat), userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	id, err := res.LastInsertId()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	b.ID = int(id)
	b.UserID = userID

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(b)
}

func getBudget(w http.ResponseWriter, userID, id int) {
	var b Budget
	var startStr, endStr string
	err := db.QueryRow("SELECT id, category, amount, start_date, end_date FROM budgets WHERE id = ? AND user_id = ?", id, userID).Scan(&b.ID, &b.Category, &b.Amount, &startStr, &endStr)
	if err == sql.ErrNoRows {
		http.Error(w, "Budget not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	startDate, err := parseTimestamp(startStr)
	if err != nil {
		log.Printf("budget start date parse error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	endDate, err := parseTimestamp(endStr)
	if err != nil {
		log.Printf("budget end date parse error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	b.StartDate = startDate
	b.EndDate = endDate
	b.UserID = userID

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(b)
}

func updateBudget(w http.ResponseWriter, r *http.Request, userID, id int) {
	var b Budget
	if !decodeJSONBody(w, r, &b) {
		return
	}

	if b.StartDate.IsZero() {
		b.StartDate = time.Now().UTC()
	} else {
		b.StartDate = b.StartDate.UTC()
	}
	if b.EndDate.IsZero() {
		b.EndDate = b.StartDate
	} else {
		b.EndDate = b.EndDate.UTC()
	}

	stmt, err := db.Prepare("UPDATE budgets SET category = ?, amount = ?, start_date = ?, end_date = ? WHERE id = ? AND user_id = ?")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	res, err := stmt.Exec(b.Category, b.Amount, b.StartDate.Format(timeFormat), b.EndDate.Format(timeFormat), id, userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "Budget not found", http.StatusNotFound)
		return
	}

	b.ID = id
	b.UserID = userID

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(b)
}

func deleteBudget(w http.ResponseWriter, userID, id int) {
	res, err := db.Exec("DELETE FROM budgets WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "Budget not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
func recurringExpensesHandler(w http.ResponseWriter, r *http.Request, userID int) {
	switch r.Method {
	case http.MethodGet:
		getRecurringExpenses(w, userID)
	case http.MethodPost:
		createRecurringExpense(w, r, userID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func recurringExpenseHandler(w http.ResponseWriter, r *http.Request, userID int) {
	idStr := strings.TrimPrefix(r.URL.Path, "/recurring-expenses/")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid recurring expense ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		getRecurringExpense(w, userID, id)
	case http.MethodPut:
		updateRecurringExpense(w, r, userID, id)
	case http.MethodDelete:
		deleteRecurringExpense(w, userID, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getRecurringExpenses(w http.ResponseWriter, userID int) {
	rows, err := db.Query("SELECT id, amount, category, note, frequency, next_due_date FROM recurring_expenses WHERE user_id = ? ORDER BY next_due_date", userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var recurringExpenses []RecurringExpense
	for rows.Next() {
		var re RecurringExpense
		var nextDueDateStr string
		if err := rows.Scan(&re.ID, &re.Amount, &re.Category, &re.Note, &re.Frequency, &nextDueDateStr); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		nextDueDate, err := parseTimestamp(nextDueDateStr)
		if err != nil {
			log.Printf("recurring expense due date parse error: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		re.NextDueDate = nextDueDate
		re.UserID = userID
		recurringExpenses = append(recurringExpenses, re)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(recurringExpenses)
}

func createRecurringExpense(w http.ResponseWriter, r *http.Request, userID int) {
	var re RecurringExpense
	if !decodeJSONBody(w, r, &re) {
		return
	}

	if !isValidFrequency(re.Frequency) {
		http.Error(w, "Invalid frequency", http.StatusBadRequest)
		return
	}
	re.Frequency = strings.ToLower(strings.TrimSpace(re.Frequency))

	if re.NextDueDate.IsZero() {
		re.NextDueDate = time.Now().UTC()
	} else {
		re.NextDueDate = re.NextDueDate.UTC()
	}

	stmt, err := db.Prepare("INSERT INTO recurring_expenses(amount, category, note, frequency, next_due_date, user_id) VALUES(?, ?, ?, ?, ?, ?)")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	res, err := stmt.Exec(re.Amount, re.Category, re.Note, re.Frequency, re.NextDueDate.Format(timeFormat), userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	id, err := res.LastInsertId()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	re.ID = int(id)
	re.UserID = userID

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(re)
}

func getRecurringExpense(w http.ResponseWriter, userID, id int) {
	var re RecurringExpense
	var nextDueDateStr string
	err := db.QueryRow("SELECT id, amount, category, note, frequency, next_due_date FROM recurring_expenses WHERE id = ? AND user_id = ?", id, userID).Scan(&re.ID, &re.Amount, &re.Category, &re.Note, &re.Frequency, &nextDueDateStr)
	if err == sql.ErrNoRows {
		http.Error(w, "Recurring expense not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	nextDueDate, err := parseTimestamp(nextDueDateStr)
	if err != nil {
		log.Printf("recurring expense due date parse error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	re.NextDueDate = nextDueDate
	re.UserID = userID

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(re)
}

func updateRecurringExpense(w http.ResponseWriter, r *http.Request, userID, id int) {
	var re RecurringExpense
	if !decodeJSONBody(w, r, &re) {
		return
	}

	if !isValidFrequency(re.Frequency) {
		http.Error(w, "Invalid frequency", http.StatusBadRequest)
		return
	}
	re.Frequency = strings.ToLower(strings.TrimSpace(re.Frequency))

	if re.NextDueDate.IsZero() {
		re.NextDueDate = time.Now().UTC()
	} else {
		re.NextDueDate = re.NextDueDate.UTC()
	}

	stmt, err := db.Prepare("UPDATE recurring_expenses SET amount = ?, category = ?, note = ?, frequency = ?, next_due_date = ? WHERE id = ? AND user_id = ?")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	res, err := stmt.Exec(re.Amount, re.Category, re.Note, re.Frequency, re.NextDueDate.Format(timeFormat), id, userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "Recurring expense not found", http.StatusNotFound)
		return
	}

	re.ID = id
	re.UserID = userID

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(re)
}

func deleteRecurringExpense(w http.ResponseWriter, userID, id int) {
	res, err := db.Exec("DELETE FROM recurring_expenses WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "Recurring expense not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func processRecurringExpenses() {
	now := time.Now().UTC()
	rows, err := db.Query("SELECT id, user_id, amount, category, note, frequency, next_due_date FROM recurring_expenses WHERE next_due_date <= ?", now.Format(timeFormat))
	if err != nil {
		log.Printf("Error querying recurring expenses: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var re RecurringExpense
		var nextDueDateStr string
		if err := rows.Scan(&re.ID, &re.UserID, &re.Amount, &re.Category, &re.Note, &re.Frequency, &nextDueDateStr); err != nil {
			log.Printf("Error scanning recurring expense: %v", err)
			continue
		}
		nextDueDate, err := parseTimestamp(nextDueDateStr)
		if err != nil {
			log.Printf("Error parsing recurring expense due date: %v", err)
			continue
		}
		re.NextDueDate = nextDueDate

		tx, err := db.Begin()
		if err != nil {
			log.Printf("Error starting transaction for recurring expense %d: %v", re.ID, err)
			continue
		}

		if _, err := tx.Exec("INSERT INTO expenses(amount, category, note, date, user_id) VALUES(?, ?, ?, ?, ?)", re.Amount, re.Category, re.Note, re.NextDueDate.Format(timeFormat), re.UserID); err != nil {
			log.Printf("Error creating expense from recurring expense %d: %v", re.ID, err)
			tx.Rollback()
			continue
		}

		var nextDueDateUpdated time.Time
		switch strings.ToLower(re.Frequency) {
		case "daily":
			nextDueDateUpdated = re.NextDueDate.AddDate(0, 0, 1)
		case "weekly":
			nextDueDateUpdated = re.NextDueDate.AddDate(0, 0, 7)
		case "monthly":
			nextDueDateUpdated = re.NextDueDate.AddDate(0, 1, 0)
		case "yearly":
			nextDueDateUpdated = re.NextDueDate.AddDate(1, 0, 0)
		default:
			nextDueDateUpdated = re.NextDueDate.AddDate(0, 0, 1)
		}

		if _, err := tx.Exec("UPDATE recurring_expenses SET next_due_date = ? WHERE id = ?", nextDueDateUpdated.Format(timeFormat), re.ID); err != nil {
			log.Printf("Error updating next due date for recurring expense %d: %v", re.ID, err)
			tx.Rollback()
			continue
		}

		if err := tx.Commit(); err != nil {
			log.Printf("Error committing recurring expense %d transaction: %v", re.ID, err)
			continue
		}
	}

	if err := rows.Err(); err != nil {
		log.Printf("Error iterating recurring expenses: %v", err)
	}
}
func incomesHandler(w http.ResponseWriter, r *http.Request, userID int) {
	switch r.Method {
	case http.MethodGet:
		getIncomes(w, userID)
	case http.MethodPost:
		createIncome(w, r, userID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func incomeHandler(w http.ResponseWriter, r *http.Request, userID int) {
	idStr := strings.TrimPrefix(r.URL.Path, "/incomes/")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid income ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		getIncome(w, userID, id)
	case http.MethodPut:
		updateIncome(w, r, userID, id)
	case http.MethodDelete:
		deleteIncome(w, userID, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getIncomes(w http.ResponseWriter, userID int) {
	rows, err := db.Query("SELECT id, amount, source, note, date FROM incomes WHERE user_id = ? ORDER BY date", userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var incomes []Income
	for rows.Next() {
		var i Income
		var dateStr string
		if err := rows.Scan(&i.ID, &i.Amount, &i.Source, &i.Note, &dateStr); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		parsedDate, err := parseTimestamp(dateStr)
		if err != nil {
			log.Printf("income date parse error: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		i.Date = parsedDate
		i.UserID = userID
		incomes = append(incomes, i)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(incomes)
}

func createIncome(w http.ResponseWriter, r *http.Request, userID int) {
	var i Income
	if !decodeJSONBody(w, r, &i) {
		return
	}

	if i.Date.IsZero() {
		i.Date = time.Now().UTC()
	} else {
		i.Date = i.Date.UTC()
	}

	if i.AccountID == nil || *i.AccountID == 0 {
		http.Error(w, "Account is required", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		log.Printf("tx begin error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	stmt, err := tx.Prepare("INSERT INTO incomes(amount, source, note, date, user_id, account_id) VALUES(?, ?, ?, ?, ?, ?)")
	if err != nil {
		tx.Rollback()
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	res, err := stmt.Exec(i.Amount, i.Source, i.Note, i.Date.Format(timeFormat), userID, i.AccountID)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	id, err := res.LastInsertId()
	if err != nil {
		tx.Rollback()
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Update Account Balance if linked
	if i.AccountID != nil {
		_, err := tx.Exec("UPDATE accounts SET balance = balance + ? WHERE id = ? AND user_id = ?", i.Amount, *i.AccountID, userID)
		if err != nil {
			tx.Rollback()
			log.Printf("failed to update account balance: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("tx commit error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	i.ID = int(id)
	i.UserID = userID

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(i)
}

func getIncome(w http.ResponseWriter, userID, id int) {
	var i Income
	var dateStr string
	err := db.QueryRow("SELECT id, amount, source, note, date FROM incomes WHERE id = ? AND user_id = ?", id, userID).Scan(&i.ID, &i.Amount, &i.Source, &i.Note, &dateStr)
	if err == sql.ErrNoRows {
		http.Error(w, "Income not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	parsedDate, err := parseTimestamp(dateStr)
	if err != nil {
		log.Printf("income date parse error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	i.Date = parsedDate
	i.UserID = userID

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(i)
}

func updateIncome(w http.ResponseWriter, r *http.Request, userID, id int) {
	var i Income
	if !decodeJSONBody(w, r, &i) {
		return
	}

	if i.Date.IsZero() {
		i.Date = time.Now().UTC()
	} else {
		i.Date = i.Date.UTC()
	}

	stmt, err := db.Prepare("UPDATE incomes SET amount = ?, source = ?, note = ?, date = ? WHERE id = ? AND user_id = ?")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	res, err := stmt.Exec(i.Amount, i.Source, i.Note, i.Date.Format(timeFormat), id, userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "Income not found", http.StatusNotFound)
		return
	}

	i.ID = id
	i.UserID = userID

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(i)
}

func deleteIncome(w http.ResponseWriter, userID, id int) {
	res, err := db.Exec("DELETE FROM incomes WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "Income not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Account Handlers

func accountsHandler(w http.ResponseWriter, r *http.Request, userID int) {
	switch r.Method {
	case http.MethodGet:
		getAccounts(w, userID)
	case http.MethodPost:
		createAccount(w, r, userID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func accountHandler(w http.ResponseWriter, r *http.Request, userID int) {
	idStr := strings.TrimPrefix(r.URL.Path, "/accounts/")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid account ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPut:
		updateAccount(w, r, userID, id)
	case http.MethodDelete:
		deleteAccount(w, userID, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getAccounts(w http.ResponseWriter, userID int) {
	rows, err := db.Query("SELECT id, name, type, balance FROM accounts WHERE user_id = ?", userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Name, &a.Type, &a.Balance); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		a.UserID = userID
		accounts = append(accounts, a)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(accounts)
}

func createAccount(w http.ResponseWriter, r *http.Request, userID int) {
	var a Account
	if !decodeJSONBody(w, r, &a) {
		return
	}

	res, err := db.Exec("INSERT INTO accounts(name, type, balance, user_id) VALUES(?, ?, ?, ?)", a.Name, a.Type, a.Balance, userID)
	if err != nil {
		log.Printf("create account error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	id, err := res.LastInsertId()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	a.ID = int(id)
	a.UserID = userID

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(a)
}

func updateAccount(w http.ResponseWriter, r *http.Request, userID, id int) {
	var a Account
	if !decodeJSONBody(w, r, &a) {
		return
	}

	// Note: Updating balance directly matches user input, though implies manual adjustment
	res, err := db.Exec("UPDATE accounts SET name = ?, type = ?, balance = ? WHERE id = ? AND user_id = ?", a.Name, a.Type, a.Balance, id, userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "Account not found", http.StatusNotFound)
		return
	}

	a.ID = id
	a.UserID = userID

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a)
}

func deleteAccount(w http.ResponseWriter, userID, id int) {
	// Optional: Check if used in transactions? For now, we rely on ON DELETE SET NULL for foreign keys if we configured that, but sqlite default might be restricted
	// Actually, the PRAGMA foreign_keys = ON is set.
	// But let's just delete. If there are transactions, they might prevent deletion if we had strict constraints, but in ensureAccountColumns we used ON DELETE SET NULL?
	// Ah, in ensureAccountColumns I used `REFERENCES accounts(id) ON DELETE SET NULL`. So it's safe.

	res, err := db.Exec("DELETE FROM accounts WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "Account not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func incomeVsExpenseReportHandler(w http.ResponseWriter, r *http.Request, userID int) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	incomeRows, err := db.Query("SELECT strftime('%Y-%m', date) AS month, SUM(amount) AS total FROM incomes WHERE user_id = ? GROUP BY month", userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer incomeRows.Close()

	reports := make(map[string]*MonthlyReport)

	for incomeRows.Next() {
		var month string
		var total float64
		if err := incomeRows.Scan(&month, &total); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		reports[month] = &MonthlyReport{Month: month, Income: total}
	}

	if err := incomeRows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	expenseRows, err := db.Query("SELECT strftime('%Y-%m', date) AS month, SUM(amount) AS total FROM expenses WHERE user_id = ? GROUP BY month", userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer expenseRows.Close()

	for expenseRows.Next() {
		var month string
		var total float64
		if err := expenseRows.Scan(&month, &total); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		if report, ok := reports[month]; ok {
			report.Expense = total
		} else {
			reports[month] = &MonthlyReport{Month: month, Expense: total}
		}
	}

	if err := expenseRows.Err(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	var months []string
	for month := range reports {
		months = append(months, month)
	}
	sort.Strings(months)

	var result []MonthlyReport
	for _, month := range months {
		result = append(result, *reports[month])
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
func parseTimestamp(value string) (time.Time, error) {
	layouts := []string{timeFormat, time.RFC3339, time.RFC3339Nano, "2006-01-02"}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, value); err == nil {
			return ts.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format: %s", value)
}
