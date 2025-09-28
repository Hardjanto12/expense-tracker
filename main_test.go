
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
	"strconv"

	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

func TestMain(m *testing.M) {
	// Set up a temporary database for testing
	db, _ = sql.Open("sqlite3", "./test.db")
	createTables()

	// Run the tests
	exitCode := m.Run()

	// Clean up the temporary database
	db.Close()
	os.Remove("./test.db")

	os.Exit(exitCode)
}

func TestCreateExpense(t *testing.T) {
	expense := Expense{
		Amount:   50.0,
		Category: "Test",
		Note:     "Test expense",
		Date:     time.Now(),
	}
	body, _ := json.Marshal(expense)

	req, _ := http.NewRequest("POST", "/expenses", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(expensesHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusCreated {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusCreated)
	}

	var createdExpense Expense
	json.Unmarshal(rr.Body.Bytes(), &createdExpense)

	if createdExpense.Amount != expense.Amount {
		t.Errorf("handler returned unexpected body: got %v want %v",
			createdExpense.Amount, expense.Amount)
	}
}

func TestGetExpenses(t *testing.T) {
	req, _ := http.NewRequest("GET", "/expenses", nil)
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(expensesHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestGetExpense(t *testing.T) {
	// First, create an expense to get
	expense := Expense{
		Amount:   100.0,
		Category: "Test Get",
		Note:     "Test get expense",
		Date:     time.Now(),
	}
	body, _ := json.Marshal(expense)

	req, _ := http.NewRequest("POST", "/expenses", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(expensesHandler)
	handler.ServeHTTP(rr, req)

	var createdExpense Expense
	json.Unmarshal(rr.Body.Bytes(), &createdExpense)

	// Now, get the expense
	getReq, _ := http.NewRequest("GET", "/expenses/"+strconv.Itoa(createdExpense.ID), nil)
	getRr := httptest.NewRecorder()
	getHandler := http.HandlerFunc(expenseHandler)
	getHandler.ServeHTTP(getRr, getReq)

	if status := getRr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestUpdateExpense(t *testing.T) {
	// First, create an expense to update
	expense := Expense{
		Amount:   120.0,
		Category: "Test Update",
		Note:     "Test update expense",
		Date:     time.Now(),
	}
	body, _ := json.Marshal(expense)

	req, _ := http.NewRequest("POST", "/expenses", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(expensesHandler)
	handler.ServeHTTP(rr, req)

	var createdExpense Expense
	json.Unmarshal(rr.Body.Bytes(), &createdExpense)

	// Now, update the expense
	updatedExpense := Expense{
		Amount:   150.0,
		Category: "Test Updated",
		Note:     "Test updated expense",
		Date:     time.Now(),
	}
	updateBody, _ := json.Marshal(updatedExpense)

	updateReq, _ := http.NewRequest("PUT", "/expenses/"+strconv.Itoa(createdExpense.ID), bytes.NewBuffer(updateBody))
	updateRr := httptest.NewRecorder()
	updateHandler := http.HandlerFunc(expenseHandler)
	updateHandler.ServeHTTP(updateRr, updateReq)

	if status := updateRr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestDeleteExpense(t *testing.T) {
	// First, create an expense to delete
	expense := Expense{
		Amount:   200.0,
		Category: "Test Delete",
		Note:     "Test delete expense",
		Date:     time.Now(),
	}
	body, _ := json.Marshal(expense)

	req, _ := http.NewRequest("POST", "/expenses", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(expensesHandler)
	handler.ServeHTTP(rr, req)

	var createdExpense Expense
	json.Unmarshal(rr.Body.Bytes(), &createdExpense)

	// Now, delete the expense
	deleteReq, _ := http.NewRequest("DELETE", "/expenses/"+strconv.Itoa(createdExpense.ID), nil)
	deleteRr := httptest.NewRecorder()
	deleteHandler := http.HandlerFunc(expenseHandler)
	deleteHandler.ServeHTTP(deleteRr, deleteReq)

	if status := deleteRr.Code; status != http.StatusNoContent {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusNoContent)
	}
}

func TestCreateBudget(t *testing.T) {
	budget := Budget{
		Category:  "Test Budget",
		Amount:    500.0,
		StartDate: time.Now(),
		EndDate:   time.Now().AddDate(0, 1, 0),
	}
	body, _ := json.Marshal(budget)

	req, _ := http.NewRequest("POST", "/budgets", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(budgetsHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusCreated {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusCreated)
	}
}

func TestGetBudgets(t *testing.T) {
	req, _ := http.NewRequest("GET", "/budgets", nil)
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(budgetsHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestGetBudget(t *testing.T) {
	// First, create a budget to get
	budget := Budget{
		Category:  "Test Get Budget",
		Amount:    600.0,
		StartDate: time.Now(),
		EndDate:   time.Now().AddDate(0, 1, 0),
	}
	body, _ := json.Marshal(budget)

	req, _ := http.NewRequest("POST", "/budgets", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(budgetsHandler)
	handler.ServeHTTP(rr, req)

	var createdBudget Budget
	json.Unmarshal(rr.Body.Bytes(), &createdBudget)

	// Now, get the budget
	getReq, _ := http.NewRequest("GET", "/budgets/"+strconv.Itoa(createdBudget.ID), nil)
	getRr := httptest.NewRecorder()
	getHandler := http.HandlerFunc(budgetHandler)
	getHandler.ServeHTTP(getRr, getReq)

	if status := getRr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestUpdateBudget(t *testing.T) {
	// First, create a budget to update
	budget := Budget{
		Category:  "Test Update Budget",
		Amount:    700.0,
		StartDate: time.Now(),
		EndDate:   time.Now().AddDate(0, 1, 0),
	}
	body, _ := json.Marshal(budget)

	req, _ := http.NewRequest("POST", "/budgets", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(budgetsHandler)
	handler.ServeHTTP(rr, req)

	var createdBudget Budget
	json.Unmarshal(rr.Body.Bytes(), &createdBudget)

	// Now, update the budget
	updatedBudget := Budget{
		Category:  "Test Updated Budget",
		Amount:    800.0,
		StartDate: time.Now(),
		EndDate:   time.Now().AddDate(0, 1, 0),
	}
	updateBody, _ := json.Marshal(updatedBudget)

	updateReq, _ := http.NewRequest("PUT", "/budgets/"+strconv.Itoa(createdBudget.ID), bytes.NewBuffer(updateBody))
	updateRr := httptest.NewRecorder()
	updateHandler := http.HandlerFunc(budgetHandler)
	updateHandler.ServeHTTP(updateRr, updateReq)

	if status := updateRr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestDeleteBudget(t *testing.T) {
	// First, create a budget to delete
	budget := Budget{
		Category:  "Test Delete Budget",
		Amount:    900.0,
		StartDate: time.Now(),
		EndDate:   time.Now().AddDate(0, 1, 0),
	}
	body, _ := json.Marshal(budget)

	req, _ := http.NewRequest("POST", "/budgets", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(budgetsHandler)
	handler.ServeHTTP(rr, req)

	var createdBudget Budget
	json.Unmarshal(rr.Body.Bytes(), &createdBudget)

	// Now, delete the budget
	deleteReq, _ := http.NewRequest("DELETE", "/budgets/"+strconv.Itoa(createdBudget.ID), nil)
	deleteRr := httptest.NewRecorder()
	deleteHandler := http.HandlerFunc(budgetHandler)
	deleteHandler.ServeHTTP(deleteRr, deleteReq)

	if status := deleteRr.Code; status != http.StatusNoContent {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusNoContent)
	}
}

func TestCreateIncome(t *testing.T) {
	income := Income{
		Amount: 1000.0,
		Source: "Test Income",
		Note:   "Test income note",
		Date:   time.Now(),
	}
	body, _ := json.Marshal(income)

	req, _ := http.NewRequest("POST", "/incomes", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(incomesHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusCreated {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusCreated)
	}
}

func TestGetIncomes(t *testing.T) {
	req, _ := http.NewRequest("GET", "/incomes", nil)
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(incomesHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestGetIncome(t *testing.T) {
	// First, create an income to get
	income := Income{
		Amount: 1100.0,
		Source: "Test Get Income",
		Note:   "Test get income note",
		Date:   time.Now(),
	}
	body, _ := json.Marshal(income)

	req, _ := http.NewRequest("POST", "/incomes", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(incomesHandler)
	handler.ServeHTTP(rr, req)

	var createdIncome Income
	json.Unmarshal(rr.Body.Bytes(), &createdIncome)

	// Now, get the income
	getReq, _ := http.NewRequest("GET", "/incomes/"+strconv.Itoa(createdIncome.ID), nil)
	getRr := httptest.NewRecorder()
	getHandler := http.HandlerFunc(incomeHandler)
	getHandler.ServeHTTP(getRr, getReq)

	if status := getRr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestUpdateIncome(t *testing.T) {
	// First, create an income to update
	income := Income{
		Amount: 1200.0,
		Source: "Test Update Income",
		Note:   "Test update income note",
		Date:   time.Now(),
	}
	body, _ := json.Marshal(income)

	req, _ := http.NewRequest("POST", "/incomes", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(incomesHandler)
	handler.ServeHTTP(rr, req)

	var createdIncome Income
	json.Unmarshal(rr.Body.Bytes(), &createdIncome)

	// Now, update the income
	updatedIncome := Income{
		Amount: 1300.0,
		Source: "Test Updated Income",
		Note:   "Test updated income note",
		Date:   time.Now(),
	}
	updateBody, _ := json.Marshal(updatedIncome)

	updateReq, _ := http.NewRequest("PUT", "/incomes/"+strconv.Itoa(createdIncome.ID), bytes.NewBuffer(updateBody))
	updateRr := httptest.NewRecorder()
	updateHandler := http.HandlerFunc(incomeHandler)
	updateHandler.ServeHTTP(updateRr, updateReq)

	if status := updateRr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestDeleteIncome(t *testing.T) {
	// First, create an income to delete
	income := Income{
		Amount: 1400.0,
		Source: "Test Delete Income",
		Note:   "Test delete income note",
		Date:   time.Now(),
	}
	body, _ := json.Marshal(income)

	req, _ := http.NewRequest("POST", "/incomes", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(incomesHandler)
	handler.ServeHTTP(rr, req)

	var createdIncome Income
	json.Unmarshal(rr.Body.Bytes(), &createdIncome)

	// Now, delete the income
	deleteReq, _ := http.NewRequest("DELETE", "/incomes/"+strconv.Itoa(createdIncome.ID), nil)
	deleteRr := httptest.NewRecorder()
	deleteHandler := http.HandlerFunc(incomeHandler)
	deleteHandler.ServeHTTP(deleteRr, deleteReq)

	if status := deleteRr.Code; status != http.StatusNoContent {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusNoContent)
	}
}

func TestCreateRecurringExpense(t *testing.T) {
	recurringExpense := RecurringExpense{
		Amount:      100.0,
		Category:    "Test Recurring",
		Note:        "Test recurring expense",
		Frequency:   "monthly",
		NextDueDate: time.Now(),
	}
	body, _ := json.Marshal(recurringExpense)

	req, _ := http.NewRequest("POST", "/recurring-expenses", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(recurringExpensesHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusCreated {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusCreated)
	}
}

func TestGetRecurringExpenses(t *testing.T) {
	req, _ := http.NewRequest("GET", "/recurring-expenses", nil)
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(recurringExpensesHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestGetRecurringExpense(t *testing.T) {
	// First, create a recurring expense to get
	recurringExpense := RecurringExpense{
		Amount:      150.0,
		Category:    "Test Get Recurring",
		Note:        "Test get recurring expense",
		Frequency:   "weekly",
		NextDueDate: time.Now(),
	}
	body, _ := json.Marshal(recurringExpense)

	req, _ := http.NewRequest("POST", "/recurring-expenses", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(recurringExpensesHandler)
	handler.ServeHTTP(rr, req)

	var createdRecurringExpense RecurringExpense
	json.Unmarshal(rr.Body.Bytes(), &createdRecurringExpense)

	// Now, get the recurring expense
	getReq, _ := http.NewRequest("GET", "/recurring-expenses/"+strconv.Itoa(createdRecurringExpense.ID), nil)
	getRr := httptest.NewRecorder()
	getHandler := http.HandlerFunc(recurringExpenseHandler)
	getHandler.ServeHTTP(getRr, getReq)

	if status := getRr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestUpdateRecurringExpense(t *testing.T) {
	// First, create a recurring expense to update
	recurringExpense := RecurringExpense{
		Amount:      200.0,
		Category:    "Test Update Recurring",
		Note:        "Test update recurring expense",
		Frequency:   "daily",
		NextDueDate: time.Now(),
	}
	body, _ := json.Marshal(recurringExpense)

	req, _ := http.NewRequest("POST", "/recurring-expenses", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(recurringExpensesHandler)
	handler.ServeHTTP(rr, req)

	var createdRecurringExpense RecurringExpense
	json.Unmarshal(rr.Body.Bytes(), &createdRecurringExpense)

	// Now, update the recurring expense
	updatedRecurringExpense := RecurringExpense{
		Amount:      250.0,
		Category:    "Test Updated Recurring",
		Note:        "Test updated recurring expense",
		Frequency:   "monthly",
		NextDueDate: time.Now(),
	}
	updateBody, _ := json.Marshal(updatedRecurringExpense)

	updateReq, _ := http.NewRequest("PUT", "/recurring-expenses/"+strconv.Itoa(createdRecurringExpense.ID), bytes.NewBuffer(updateBody))
	updateRr := httptest.NewRecorder()
	updateHandler := http.HandlerFunc(recurringExpenseHandler)
	updateHandler.ServeHTTP(updateRr, updateReq)

	if status := updateRr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestDeleteRecurringExpense(t *testing.T) {
	// First, create a recurring expense to delete
	recurringExpense := RecurringExpense{
		Amount:      300.0,
		Category:    "Test Delete Recurring",
		Note:        "Test delete recurring expense",
		Frequency:   "yearly",
		NextDueDate: time.Now(),
	}
	body, _ := json.Marshal(recurringExpense)

	req, _ := http.NewRequest("POST", "/recurring-expenses", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(recurringExpensesHandler)
	handler.ServeHTTP(rr, req)

	var createdRecurringExpense RecurringExpense
	json.Unmarshal(rr.Body.Bytes(), &createdRecurringExpense)

	// Now, delete the recurring expense
	deleteReq, _ := http.NewRequest("DELETE", "/recurring-expenses/"+strconv.Itoa(createdRecurringExpense.ID), nil)
	deleteRr := httptest.NewRecorder()
	deleteHandler := http.HandlerFunc(recurringExpenseHandler)
	deleteHandler.ServeHTTP(deleteRr, deleteReq)

	if status := deleteRr.Code; status != http.StatusNoContent {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusNoContent)
	}
}

func TestAggregatesHandler(t *testing.T) {
	// Test totals_by_month
	req, _ := http.NewRequest("GET", "/expenses/aggregates?query=totals_by_month", nil)
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(aggregatesHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code for totals_by_month: got %v want %v",
			status, http.StatusOK)
	}

	// Test totals_by_category
	req, _ = http.NewRequest("GET", "/expenses/aggregates?query=totals_by_category", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code for totals_by_category: got %v want %v",
			status, http.StatusOK)
	}
}

func TestIncomeVsExpenseReportHandler(t *testing.T) {
	req, _ := http.NewRequest("GET", "/reports/income-vs-expense", nil)
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(incomeVsExpenseReportHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}
