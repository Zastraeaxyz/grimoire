// Package chastisafe talks to the ChastiSafe lock service at
// https://chastisafe.com.
//
// ChastiSafe does not publish an API. The endpoints and payloads in this
// package come from the service's Flutter web client and from live requests
// against a real account. Keep the transport details in this package so a
// server change only has one place to fix.
//
// Authentication is a per-account developer code sent in the chastisafedev
// header. The code is also sent in the chastisafe cookie because the live
// service accepts that form even when a header-only request fails. The same
// value is stored in the chastisafe cookie by the web app. Treat it as a
// password: never log it, never commit it, and only store it for the account
// that owns it.
//
// # Example
//
//	client, err := chastisafe.New(chastisafe.Options{Token: code})
//	if err != nil {
//		return err
//	}
//	me, err := client.Who(ctx)
//	if err != nil {
//		return err
//	}
//	if !me.OwnedBy(discordUserID) {
//		return errors.New("this ChastiSafe code belongs to another Discord user")
//	}
//	locks, err := client.Loaded(ctx)
//	if err != nil {
//		return err
//	}
//	for _, lock := range locks {
//		fmt.Println(lock.LockName, lock.Remaining())
//	}
//
// The package is safe for concurrent use. A Client is bound to one account;
// use [Client.WithToken] to share the HTTP client with another account.
package chastisafe
