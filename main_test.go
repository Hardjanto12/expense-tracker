package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

var (
	testSessionCookie *http.Cookie
	testUserID        int
)

func TestMain(m *testing.M) {
	var err error
	db, err = sql.Open("sqlite3", "./test.db")
	if err != nil {
		panic(err)
	}

	if err := createTables(); err != nil {
		panic(err)
	}

	if err := seedTestUser(); err != nil {
		panic(err)
	}

	exitCode := m.Run()

	db.Close()
	os.Remove("./test.db")

	os.Exit(exitCode)
}

func seedTestUser() error {
	payload := credentials{
		Email:    "tester@example.com",
		Password: "VerySecurePass123!",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	registerHandler(rr, req)

	if rr.Code != http.StatusCreated {
		return fmt.Errorf("unexpected register status: %d", rr.Code)
	}

	res := rr.Result()
	for _, c := range res.Cookies() {
		if c.Name == sessionCookieName {
			copied := *c
			testSessionCookie = &copied
			break
		}
	}

	if testSessionCookie == nil {
		return fmt.Errorf("session cookie not set")
	}

	if err := db.QueryRow("SELECT id FROM users WHERE email = ?", strings.ToLower(payload.Email)).Scan(&testUserID); err != nil {
		return err
	}

	return nil
}
func resetData(t *testing.T) {
	tables := []string{"expenses", "budgets", "recurring_expenses", "incomes"}
	for _, table := range tables {
		if _, err := db.Exec("DELETE FROM "+table+" WHERE user_id = ?", testUserID); err != nil {
			t.Fatalf("cleanup %s: %v", table, err)
		}
	}
}

func authedRequest(method, target string, payload interface{}) *http.Request {
	var reader *bytes.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			panic(err)
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, target, reader)
	req.AddCookie(testSessionCookie)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func performAuthed(handler http.HandlerFunc, method, target string, payload interface{}) *httptest.ResponseRecorder {
	req := authedRequest(method, target, payload)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func callAuthed(handler authedHandler, method, target string, payload interface{}) *httptest.ResponseRecorder {
	return performAuthed(withAuth(handler), method, target, payload)
}

func decodeBody[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	var out T
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response failed: %v (body: %s)", err, rr.Body.String())
	}
	return out
}

func expectStatus(t *testing.T, rr *httptest.ResponseRecorder, want int) {
	if rr.Code != want {
		t.Fatalf("unexpected status: got %d want %d (body: %s)", rr.Code, want, rr.Body.String())
	}
}
func TestExpenseLifecycle(t *testing.T) {
	resetData(t)

	now := time.Now().UTC().Truncate(time.Second)
	expense := Expense{
		Amount:   50.5,
		Category: "Test",
		Note:     "Initial expense",
		Date:     now,
	}

	createRR := callAuthed(expensesHandler, http.MethodPost, "/expenses", expense)
	expectStatus(t, createRR, http.StatusCreated)
	created := decodeBody[Expense](t, createRR)
	if created.ID == 0 {
		t.Fatalf("expected expense ID to be set")
	}

	listRR := callAuthed(expensesHandler, http.MethodGet, "/expenses", nil)
	expectStatus(t, listRR, http.StatusOK)
	list := decodeBody[[]Expense](t, listRR)
	if len(list) != 1 {
		t.Fatalf("expected 1 expense, got %d", len(list))
	}

	getRR := callAuthed(expenseHandler, http.MethodGet, fmt.Sprintf("/expenses/%d", created.ID), nil)
	expectStatus(t, getRR, http.StatusOK)
	fetched := decodeBody[Expense](t, getRR)
	if fetched.Amount != expense.Amount {
		t.Fatalf("expected amount %.2f got %.2f", expense.Amount, fetched.Amount)
	}

	updatedExpense := Expense{
		Amount:   75,
		Category: "Updated",
		Note:     "Updated expense",
		Date:     now.Add(24 * time.Hour),
	}

	updateRR := callAuthed(expenseHandler, http.MethodPut, fmt.Sprintf("/expenses/%d", created.ID), updatedExpense)
	expectStatus(t, updateRR, http.StatusOK)
	updated := decodeBody[Expense](t, updateRR)
	if updated.Category != "Updated" {
		t.Fatalf("expected category Updated got %s", updated.Category)
	}

	deleteRR := callAuthed(expenseHandler, http.MethodDelete, fmt.Sprintf("/expenses/%d", created.ID), nil)
	expectStatus(t, deleteRR, http.StatusNoContent)

	missingRR := callAuthed(expenseHandler, http.MethodGet, fmt.Sprintf("/expenses/%d", created.ID), nil)
	if missingRR.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", missingRR.Code)
	}
}
func TestExpenseAggregates(t *testing.T) {
	resetData(t)

	base := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	expenses := []Expense{
		{Amount: 120, Category: "Food", Note: "January food", Date: base},
		{Amount: 80, Category: "Travel", Note: "February travel", Date: base.AddDate(0, 1, 0)},
		{Amount: 30, Category: "Food", Note: "January snack", Date: base.Add(48 * time.Hour)},
	}

	for _, e := range expenses {
		rr := callAuthed(expensesHandler, http.MethodPost, "/expenses", e)
		expectStatus(t, rr, http.StatusCreated)
	}

	monthRR := callAuthed(aggregatesHandler, http.MethodGet, "/expenses/aggregates?query=totals_by_month", nil)
	expectStatus(t, monthRR, http.StatusOK)
	totalsByMonth := decodeBody[map[string]float64](t, monthRR)
	if len(totalsByMonth) != 2 {
		t.Fatalf("expected totals for 2 months, got %d", len(totalsByMonth))
	}
	if totalsByMonth["2024-01"] != 150 {
		t.Fatalf("unexpected January total: %.2f", totalsByMonth["2024-01"])
	}

	categoryRR := callAuthed(aggregatesHandler, http.MethodGet, "/expenses/aggregates?query=totals_by_category", nil)
	expectStatus(t, categoryRR, http.StatusOK)
	totalsByCategory := decodeBody[map[string]float64](t, categoryRR)
	if len(totalsByCategory) != 2 {
		t.Fatalf("expected totals for 2 categories, got %d", len(totalsByCategory))
	}
	if totalsByCategory["Food"] != 150 {
		t.Fatalf("unexpected Food total: %.2f", totalsByCategory["Food"])
	}
}
func TestBudgetLifecycle(t *testing.T) {
	resetData(t)

	start := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	budget := Budget{
		Category:  "Utilities",
		Amount:    200,
		StartDate: start,
		EndDate:   start.AddDate(0, 1, 0),
	}

	createRR := callAuthed(budgetsHandler, http.MethodPost, "/budgets", budget)
	expectStatus(t, createRR, http.StatusCreated)
	created := decodeBody[Budget](t, createRR)

	listRR := callAuthed(budgetsHandler, http.MethodGet, "/budgets", nil)
	expectStatus(t, listRR, http.StatusOK)
	list := decodeBody[[]Budget](t, listRR)
	if len(list) != 1 {
		t.Fatalf("expected 1 budget, got %d", len(list))
	}

	getRR := callAuthed(budgetHandler, http.MethodGet, fmt.Sprintf("/budgets/%d", created.ID), nil)
	expectStatus(t, getRR, http.StatusOK)
	fetched := decodeBody[Budget](t, getRR)
	if fetched.Category != budget.Category {
		t.Fatalf("expected category %s got %s", budget.Category, fetched.Category)
	}

	updated := Budget{
		Category:  "Utilities",
		Amount:    250,
		StartDate: budget.StartDate,
		EndDate:   budget.EndDate.AddDate(0, 0, 15),
	}

	updateRR := callAuthed(budgetHandler, http.MethodPut, fmt.Sprintf("/budgets/%d", created.ID), updated)
	expectStatus(t, updateRR, http.StatusOK)
	updatedResp := decodeBody[Budget](t, updateRR)
	if updatedResp.Amount != 250 {
		t.Fatalf("expected amount 250 got %.2f", updatedResp.Amount)
	}

	deleteRR := callAuthed(budgetHandler, http.MethodDelete, fmt.Sprintf("/budgets/%d", created.ID), nil)
	expectStatus(t, deleteRR, http.StatusNoContent)
}
func TestRecurringExpenseLifecycle(t *testing.T) {
	resetData(t)

	next := time.Now().UTC().Truncate(time.Second)
	recurring := RecurringExpense{
		Amount:      45,
		Category:    "Subscription",
		Note:        "Monthly service",
		Frequency:   "monthly",
		NextDueDate: next,
	}

	createRR := callAuthed(recurringExpensesHandler, http.MethodPost, "/recurring-expenses", recurring)
	expectStatus(t, createRR, http.StatusCreated)
	created := decodeBody[RecurringExpense](t, createRR)

	listRR := callAuthed(recurringExpensesHandler, http.MethodGet, "/recurring-expenses", nil)
	expectStatus(t, listRR, http.StatusOK)
	list := decodeBody[[]RecurringExpense](t, listRR)
	if len(list) != 1 {
		t.Fatalf("expected 1 recurring expense, got %d", len(list))
	}

	getRR := callAuthed(recurringExpenseHandler, http.MethodGet, fmt.Sprintf("/recurring-expenses/%d", created.ID), nil)
	expectStatus(t, getRR, http.StatusOK)

	updated := RecurringExpense{
		Amount:      60,
		Category:    "Subscription",
		Note:        "Monthly service upgraded",
		Frequency:   "yearly",
		NextDueDate: next.AddDate(0, 1, 0),
	}

	updateRR := callAuthed(recurringExpenseHandler, http.MethodPut, fmt.Sprintf("/recurring-expenses/%d", created.ID), updated)
	expectStatus(t, updateRR, http.StatusOK)
	updatedResp := decodeBody[RecurringExpense](t, updateRR)
	if updatedResp.Frequency != "yearly" {
		t.Fatalf("expected yearly frequency")
	}

	deleteRR := callAuthed(recurringExpenseHandler, http.MethodDelete, fmt.Sprintf("/recurring-expenses/%d", created.ID), nil)
	expectStatus(t, deleteRR, http.StatusNoContent)
}
func TestIncomeLifecycle(t *testing.T) {
	resetData(t)

	now := time.Now().UTC().Truncate(time.Second)
	income := Income{
		Amount: 900,
		Source: "Salary",
		Note:   "Monthly salary",
		Date:   now,
	}

	createRR := callAuthed(incomesHandler, http.MethodPost, "/incomes", income)
	expectStatus(t, createRR, http.StatusCreated)
	created := decodeBody[Income](t, createRR)

	listRR := callAuthed(incomesHandler, http.MethodGet, "/incomes", nil)
	expectStatus(t, listRR, http.StatusOK)
	list := decodeBody[[]Income](t, listRR)
	if len(list) != 1 {
		t.Fatalf("expected 1 income, got %d", len(list))
	}

	getRR := callAuthed(incomeHandler, http.MethodGet, fmt.Sprintf("/incomes/%d", created.ID), nil)
	expectStatus(t, getRR, http.StatusOK)

	updated := Income{
		Amount: 950,
		Source: "Salary",
		Note:   "Salary with bonus",
		Date:   now.AddDate(0, 0, 1),
	}

	updateRR := callAuthed(incomeHandler, http.MethodPut, fmt.Sprintf("/incomes/%d", created.ID), updated)
	expectStatus(t, updateRR, http.StatusOK)
	updatedResp := decodeBody[Income](t, updateRR)
	if updatedResp.Amount != 950 {
		t.Fatalf("expected amount 950 got %.2f", updatedResp.Amount)
	}

	deleteRR := callAuthed(incomeHandler, http.MethodDelete, fmt.Sprintf("/incomes/%d", created.ID), nil)
	expectStatus(t, deleteRR, http.StatusNoContent)
}
func TestIncomeVsExpenseReport(t *testing.T) {
	resetData(t)

	base := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)

	incomes := []Income{
		{Amount: 1500, Source: "Salary", Note: "April salary", Date: base},
		{Amount: 300, Source: "Freelance", Note: "Side gig", Date: base.AddDate(0, 1, 0)},
	}
	expenses := []Expense{
		{Amount: 600, Category: "Rent", Note: "April rent", Date: base.AddDate(0, 0, 2)},
		{Amount: 200, Category: "Groceries", Note: "May groceries", Date: base.AddDate(0, 1, 5)},
	}

	for _, income := range incomes {
		rr := callAuthed(incomesHandler, http.MethodPost, "/incomes", income)
		expectStatus(t, rr, http.StatusCreated)
	}
	for _, expense := range expenses {
		rr := callAuthed(expensesHandler, http.MethodPost, "/expenses", expense)
		expectStatus(t, rr, http.StatusCreated)
	}

	reportRR := callAuthed(incomeVsExpenseReportHandler, http.MethodGet, "/reports/income-vs-expense", nil)
	expectStatus(t, reportRR, http.StatusOK)
	report := decodeBody[[]MonthlyReport](t, reportRR)
	if len(report) != 2 {
		t.Fatalf("expected report for 2 months, got %d", len(report))
	}

	if report[0].Income <= 0 || report[0].Expense <= 0 {
		t.Fatalf("expected positive totals in report: %+v", report)
	}
}
