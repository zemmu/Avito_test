package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/go-sql-driver/mysql"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type User struct {
	Id      int64
	Balance float64
}

type Transaction struct {
	Id      int64
	IdUser  int64
	IdServ  int64
	IdOrder int64
	Cost    float64
	Date    string
}
type TransactModel struct {
	Db *sql.DB
}

type Response struct {
	Key string `json:"key"`

	IdUser     int64   `json:"idUser"`
	MoneyCount float64 `json:"moneyCount"`
	IdServ     int64   `json:"idServ"`
	IdOrder    int64   `json:"idOrder"`

	// Для отчета
	Period string `json:"period"`
	// Для сортировки при выборе транзакций
	SortBy     string `json:"column"`
	Keyword    string `json:"keyword"`
	Pagination int16  `json:"pagination"`
}

func DBConnect() (*sql.DB, error) {
	mysqlCfg := mysql.Config{
		User:                 "root",
		Passwd:               "123",
		Net:                  "tcp",
		Addr:                 "dockerdev-sql:3306",
		DBName:               "dataset",
		AllowNativePasswords: true,
	}
	var conn *sql.DB
	conn, err := sql.Open("mysql", mysqlCfg.FormatDSN())
	return conn, err
}

func SelectUsers() ([]User, error) {
	db, err := DBConnect()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, _ := db.Query("SELECT users.id, balance FROM users, wallets WHERE users.id=wallets.id_user")
	var users []User
	for rows.Next() {
		var user User
		_ = rows.Scan(&user.Id, &user.Balance)
		users = append(users, user)
	}
	return users, nil
}

func Deposit(id int64, moneyCount float64) {
	db, err := DBConnect()
	if err != nil {
		return
	}
	defer db.Close()

	StmtSelectUser, err := db.Prepare("SELECT `id` FROM `wallets` WHERE `id_user`=?")
	StmtSelectBalance, err := db.Prepare("SELECT `balance` FROM `wallets` WHERE `id_user`=?")
	StmtUpdateBalance, err := db.Prepare("UPDATE `wallets` SET `balance`=? WHERE `id_user`=?")

	var user User
	err = StmtSelectUser.QueryRow(id).Scan(&user.Id)
	if err == sql.ErrNoRows {
		_, err = db.Exec("INSERT INTO `wallets` (`id_user`) VALUE (?)", id)
		if err != nil {
			return
		}
	} else if err != nil {
		return
	}
	_ = StmtSelectBalance.QueryRow(id).Scan(&user.Balance)
	user.Balance += moneyCount
	_, _ = StmtUpdateBalance.Exec(user.Balance, id)
	return
}

func BuyService(idUser int64, idService int64, idOrder int64, price float64) {
	db, err := DBConnect()
	if err != nil {
		return
	}
	defer db.Close()
	var (
		us                User
		betweenCountMoney float64
	)

	_ = db.QueryRow("SELECT `balance` FROM `wallets` WHERE `id_user`=?", idUser).Scan(&us.Balance)
	betweenCountMoney = us.Balance - price
	if betweenCountMoney >= 0 {
		// Списание средств со счета пользователя
		_, err = db.Exec("UPDATE `wallets` SET `balance`=? WHERE `id_user`=?", betweenCountMoney, idUser)
		// Запись средств на резервный счет
		_, err = db.Exec("UPDATE `companyWallets` SET `balance`=? WHERE `id`=3", price)
		if err != nil {
			return
		}
		// Вызов признания
		RegulateReserve(rand.Intn(3), idUser, idService, idOrder, price)
	} else {
		return
	}
}

// RegulateReserve Подобие признания выручки.
func RegulateReserve(status int, idUser int64, idService int64, idOrder int64, price float64) {
	db, err := DBConnect()
	if err != nil {
		return
	}
	defer db.Close()

	StmtSelectReserveBalance, err := db.Prepare("SELECT `balance` FROM `companyWallets` WHERE `id`=?")
	var (
		reserveWallet User
		mainWallet    User
		userWallet    User
	)

	// Выбор средств с резервного/основного счетов
	_ = StmtSelectReserveBalance.QueryRow(3).Scan(&reserveWallet.Balance)
	_ = StmtSelectReserveBalance.QueryRow(2).Scan(&mainWallet.Balance)
	// Списание с резервного счета
	_, _ = db.Exec("UPDATE `companyWallets` SET `balance`=? WHERE `id`=3", reserveWallet.Balance-price)

	switch status {
	// Success
	case 0:
	case 1:
		// Запись средств на основной счет
		_, _ = db.Exec("UPDATE `companyWallets` SET `balance`=? WHERE `id`=2", mainWallet.Balance+reserveWallet.Balance)
		// Запись в БД для бухгалтерии
		timeTransaction := strconv.FormatInt(time.Now().Unix(), 10)
		_, _ = db.Exec("INSERT INTO `transactions` (`id_user`, `id_serv`, `id_order`, `cost`, `date_time`) VALUES (?, ?, ?, ?, ?)", idUser, idService, idOrder, price, timeTransaction)
		return
	// Fail
	case 2:
		// Выбор баланса пользователя
		_ = db.QueryRow("SELECT `balance` FROM `wallets` WHERE `id_user`=?", idUser).Scan(&userWallet.Balance)
		// Вернуть средства пользователю
		_, _ = db.Exec("UPDATE `wallets` SET `balance`=? WHERE `id_user`=?", userWallet.Balance+price, idUser)
		return
	default:
		return
	}
}

func GetUserBalance(id int64) float64 {
	db, err := DBConnect()
	if err != nil {
		return -1
	}
	defer db.Close()

	var user User
	_ = db.QueryRow("SELECT `balance` FROM `wallets` WHERE wallets.id_user=?", id).Scan(&user.Balance)
	return user.Balance
}

// Additional
// Получение отчета (№1)
func GetReport(startpoint string) []string {
	db, err := DBConnect()
	if err != nil {
		return nil
	}
	defer db.Close()

	var (
		endMonth int64
		endYear  int64
		intoRep  []string
	)
	layout := "2006-01"
	startYear, startMonth := SeparateDate(startpoint)
	if startMonth == 12 {
		endMonth = 1
		endYear = startYear + 1
	} else {
		endMonth = startMonth + 1
		endYear = startYear
	}

	stpoint, _ := time.Parse(layout, JoinDate(startYear, startMonth))
	endpoint, _ := time.Parse(layout, JoinDate(endYear, endMonth))
	endpoint = time.Unix(endpoint.Unix()-1, 0)

	rows, err1 := db.Query("SELECT `id_serv`, sum(`cost`) FROM `transactions` WHERE `date_time` BETWEEN ? AND ? GROUP BY `id_serv`, `cost`", stpoint.Unix(), endpoint.Unix())
	if err1 != nil {
		return nil
	}

	for rows.Next() {
		var (
			idServ int64
			cost   float64
			str    string
		)
		err2 := rows.Scan(&idServ, &cost)
		if err2 != nil {
			return nil
		} else {
			str = "ID услуги: " + strconv.FormatInt(idServ, 10) + " ; Общая сумма выручки " + strconv.FormatInt(int64(cost), 10)
		}
		intoRep = append(intoRep, str)
	}

	return intoRep
}

func SeparateDate(ym string) (int64, int64) {
	dates := strings.Split(ym, "-")
	yearstr := dates[0]
	year, _ := strconv.ParseInt(yearstr, 10, 64)
	month, _ := strconv.ParseInt(dates[1], 10, 64)
	return year, month
}

func JoinDate(year int64, month int64) string {
	yearStr := strconv.FormatInt(year, 10)
	monthStr := strconv.FormatInt(month, 10)
	if month < 10 {
		monthStr = "0" + monthStr
	}
	return yearStr + "-" + monthStr
}

// Получение списка транзакций (№2)
func GetTransactions(column string, keyword string, pagi int16) [][]Transaction {
	db, err := DBConnect()
	if err != nil {
		return [][]Transaction{}
	}
	defer db.Close()

	var transactions [][]Transaction
	transactModel := TransactModel{Db: db}

	transactions = transactModel.Order(column, keyword, pagi)

	return transactions
}

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Погнали...")
		var jsonData Response
		if err := json.NewDecoder(r.Body).Decode(&jsonData); err != nil {
			fmt.Fprintln(w, err)
		}

		db, _ := DBConnect()
		defer db.Close()
		fmt.Fprintln(w, "К БД подключились")

		//var transactions [][]Transaction
		//var transactions []Transaction

		//transactModel := TransactModel{Db: db}
		//
		//transactions = transactModel.Order(jsonData.SortBy, jsonData.Keyword, jsonData.Pagination)
		//fmt.Fprintln(w, transactions)

		Regulator := func(key string) string {
			switch key {
			case "Deposit":
				Deposit(jsonData.IdUser, jsonData.MoneyCount)
				return ""
			case "BuyService":
				if jsonData.IdServ != 0 && jsonData.IdOrder != 0 {
					BuyService(jsonData.IdUser, jsonData.IdServ, jsonData.IdOrder, jsonData.MoneyCount)
				}
				return ""
			case "GetBalance":
				result, _ := json.Marshal(GetUserBalance(jsonData.IdUser))
				return string(result)

			case "GetReport":
				end, _ := json.Marshal(GetReport(jsonData.Period))
				return string(end)
				//return ""
			case "GetTransacts":
				result, _ := json.Marshal(GetTransactions(jsonData.SortBy, jsonData.Keyword, jsonData.Pagination))
				return string(result)
			default:
				str, _ := json.Marshal("Такого функционала нет")
				return string(str)
			}
		}

		fmt.Fprintln(w, Regulator(jsonData.Key))

		users, _ := SelectUsers()
		str, _ := json.Marshal(users)
		fmt.Fprintln(w, "\n \n")
		fmt.Fprintln(w, string(str))
	})
	_ = http.ListenAndServe(":8000", nil)

}

// Models

func (transactModel TransactModel) Order(column string, keyword string, pagi int16) [][]Transaction {
	var (
		rows             *sql.Rows
		transactions     [][]Transaction
		transactionsPart []Transaction
		counter          int16
		err              error
	)
	if column != "" && keyword != "" {
		if column == "cost" {
			if keyword == "ASC" {
				rows, err = transactModel.Db.Query("SELECT * FROM `transactions` ORDER BY `cost` ASC")
			} else {
				if keyword == "DESC" {
					rows, err = transactModel.Db.Query("SELECT * FROM `transactions` ORDER BY `cost` DESC ")
				}
			}
		} else if column == "date_time" {
			if keyword == "ASC" {
				rows, err = transactModel.Db.Query("SELECT * FROM `transactions` ORDER BY `date_time` ASC")
			} else {
				if keyword == "DESC" {
					rows, err = transactModel.Db.Query("SELECT * FROM `transactions` ORDER BY `date_time` DESC ")
				}
			}
		}
	} else {
		rows, err = transactModel.Db.Query("select * from `transactions`")
	}
	if err != nil {
		return nil
	}
	counter = 0

	for rows.Next() {
		var transaction Transaction
		err2 := rows.Scan(&transaction.Id, &transaction.IdUser, &transaction.IdServ, &transaction.IdOrder, &transaction.Cost, &transaction.Date)

		if err2 != nil {
			return nil
		} else {
			i, _ := strconv.ParseInt(transaction.Date, 10, 64)
			tm := time.Unix(i, 0)
			transaction.Date = tm.Format("02-01-2006 15:04:05")
			transactionsPart = append(transactionsPart, transaction)
			counter++
			if counter == pagi {
				transactions = append(transactions, transactionsPart)
				transactionsPart = []Transaction{}
				counter = 0
			}
		}
	}
	if len(transactionsPart) > 0 {
		transactions = append(transactions, transactionsPart)
	}
	return transactions
}
