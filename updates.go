package main

import (
	"encoding/json"
	"fmt"
	"github.com/L11R/go-chatwars-api"
	"github.com/arangodb/go-driver"
	"time"
)

func HandleUpdate(update cwapi.Response) error {
	switch update.GetActionEnum() {
	case cwapi.CreateAuthCode:
		if update.GetResultEnum() == cwapi.Ok {
			_, err := usersCol.CreateDocument(nil, user{
				ID: fmt.Sprint(update.Payload.ResCreateAuthCode.UserID),
			})
			if err != nil && err.(driver.ArangoError).ErrorNum == 1210 {
				// pass it
			} else if err != nil {
				return err
			}
		}

		if waiter, found := waiters.Load(update.Payload.ResCreateAuthCode.UserID); found {
			// found? send it to waiter channel
			if update.Result == "Ok" {
				waiter.(chan map[string]string) <- map[string]string{"createAuthCode": "waitingCode"}
			} else {
				waiter.(chan map[string]string) <- map[string]string{"createAuthCode": "internalError"}
			}

			// trying to prevent memory leak
			close(waiter.(chan map[string]string))
		}
	case cwapi.GrantToken:
		if update.GetResultEnum() == cwapi.Ok {
			_, err := usersCol.UpdateDocument(
				nil,
				fmt.Sprint(update.Payload.ResGrantToken.UserID),
				user{
					Token: update.Payload.ResGrantToken.Token,
				},
			)
			if err != nil {
				return err
			}
		}

		if waiter, found := waiters.Load(update.Payload.ResGrantToken.UserID); found {
			// found? send it to waiter channel
			if update.Result == "Ok" {
				waiter.(chan map[string]string) <- map[string]string{"grantToken": "success"}
			} else {
				waiter.(chan map[string]string) <- map[string]string{"grantToken": "internalError"}
			}

			// trying to prevent memory leak
			close(waiter.(chan map[string]string))
		}
	case cwapi.RequestStock:
		if update.GetResultEnum() == cwapi.Ok {
			_, err := db.Query(
				nil,
				`FOR u IN users
					FILTER u._key == @key 
						UPDATE u WITH {
							stock: @stock
						} IN users OPTIONS {mergeObjects: false}`,
				map[string]interface{}{
					"key":   fmt.Sprint(update.Payload.ResRequestStock.UserID),
					"stock": update.Payload.ResRequestStock.Stock,
				},
			)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func HandlePages(pages []cwapi.YellowPage) error {
	b, err := json.Marshal(pages)
	if err != nil {
		return err
	}

	_, err = db.Transaction(
		nil,
		`function (params) {
    		const db = require("@arangodb").db;
			// Truncate shops table
			db.shops.truncate();
			
			// Parse from passed JSON
			const shops = JSON.parse(params[0]);

			// Insert new shops
			for (let i = 0; i < shops.length; i++) {
				db.shops.insert(shops[i])
			};
		}`,
		&driver.TransactionOptions{
			WaitForSync:      true,
			WriteCollections: []string{"shops"},
			Params:           []interface{}{string(b)},
		},
	)
	if err != nil {
		return err
	}

	return nil
}

func UpdateStocks() error {
	for {
		// Get tokens cursor
		cursor, err := db.Query(
			nil,
			`FOR u IN users
						RETURN u`,
			nil,
		)
		if err != nil {
			return err
		}

		// Don't forget to close
		defer cursor.Close()

		// Get all tokens
		for {
			var user user
			_, err := cursor.ReadDocument(nil, &user)
			if driver.IsNoMoreDocuments(err) {
				break
			} else if err != nil {
				return err
			}

			// Request new stock
			if err := client.RequestStock(user.Token); err != nil {
				return err
			}
		}

		// Wait before new update
		time.Sleep(30 * time.Second)
	}
}
