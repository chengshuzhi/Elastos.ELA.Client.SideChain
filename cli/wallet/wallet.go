package wallet

import (
	"os"
	"fmt"
	"errors"
	"strings"

	"ELAClient/crypto"
	. "ELAClient/common"
	"ELAClient/common/log"
	walt "ELAClient/wallet"
	. "ELAClient/cli/common"
	"ELAClient/common/password"

	"github.com/urfave/cli"
)

const (
	MinMultiSignKeys = 3
)

func printLine() {
	for i := 0; i < 80; i++ {
		fmt.Print("=")
	}
	fmt.Println()
}

func createWallet(password []byte) error {
	defer ClearBytes(password, len(password))

	_, err := walt.Create(getPassword(password, true))
	if err != nil {
		return err
	}
	return showAccountInfo(password)
}

func addAccount(wallet walt.Wallet, content string) error {
	// Check content type
	if !strings.Contains(content, ",") { // standard account
		keyBytes, err := HexStringToBytes(strings.TrimSpace(content))
		if err != nil {
			return err
		}
		publicKey, err := crypto.DecodePoint(keyBytes)
		if err != nil {
			return err
		}
		programHash, err := wallet.AddStandardAddress(publicKey)
		if err != nil {
			return err
		}
		address, err := programHash.ToAddress()
		if err != nil {
			return err
		}
		fmt.Println(address)
	} else { // multi sign account
		publicKeys := strings.Split(content, ",")
		if len(publicKeys) < MinMultiSignKeys {
			return errors.New("public keys is not enough")
		}
		var keys []*crypto.PubKey
		for _, v := range publicKeys {
			keyBytes, err := HexStringToBytes(strings.TrimSpace(v))
			if err != nil {
				return err
			}
			publicKey, err := crypto.DecodePoint(keyBytes)
			if err != nil {
				return err
			}
			keys = append(keys, publicKey)
		}
		programHash, err := wallet.AddMultiSignAddress(keys)
		if err != nil {
			return err
		}
		address, err := programHash.ToAddress()
		if err != nil {
			return err
		}
		fmt.Println(address)
	}
	// When add a new address, sync chain data to find UTXOs of this address
	wallet.CurrentHeight(walt.ResetHeightCode)
	wallet.SyncChainData()

	return nil
}

func changePassword(password []byte, wallet walt.Wallet) error {
	// Verify old password
	oldPassword := getPassword(password, false)
	err := wallet.VerifyPassword(oldPassword)
	if err != nil {
		return err
	}
	defer ClearBytes(oldPassword, len(oldPassword))

	// Input new password
	fmt.Println("# input new password #")
	newPassword := getPassword(nil, true)
	if err := wallet.ChangePassword(oldPassword, newPassword); err != nil {
		return errors.New("failed to change password")
	}
	defer ClearBytes(newPassword, len(newPassword))

	fmt.Println("password changed successful")

	return nil
}

func showAccountInfo(password []byte) error {
	password = getPassword(password, false)
	defer ClearBytes(password, len(password))

	keyStore, err := walt.OpenKeyStore(password)
	if err != nil {
		return err
	}
	programHash := keyStore.GetProgramHash()
	address, _ := programHash.ToAddress()
	publicKey := keyStore.GetPublicKey(password)
	publicKeyBytes, _ := publicKey.EncodePoint(true)

	printLine()
	fmt.Println("Address:     ", address)
	fmt.Println("Public Key:  ", BytesToHexString(publicKeyBytes))
	fmt.Println("ProgramHash: ", BytesToHexString(programHash.ToArrayReverse()))
	printLine()

	return nil
}

func listBalanceInfo(wallet walt.Wallet) error {
	wallet.SyncChainData()
	addresses, err := wallet.GetAddresses()
	if err != nil {
		log.Error("Get addresses error:", err)
		return errors.New("get wallet addresses failed")
	}
	printLine()
	for _, address := range addresses {
		balance := Fixed64(0)
		programHash := address.ProgramHash
		UTXOs, err := wallet.GetAddressUTXOs(programHash)
		if err != nil {
			return errors.New("get " + address.Address + " UTXOs failed")
		}
		for _, utxo := range UTXOs {
			balance += *utxo.Amount
		}
		fmt.Println("Address:     ", address.Address)
		fmt.Println("ProgramHash: ", BytesToHexString(address.ProgramHash.ToArrayReverse()))
		fmt.Println("Balance:     ", balance.String())

		printLine()
	}
	return nil
}

func getPassword(passwd []byte, confirmed bool) []byte {
	var tmp []byte
	var err error
	if len(passwd) > 0 {
		tmp = []byte(passwd)
	} else {
		if confirmed {
			tmp, err = password.GetConfirmedPassword()
		} else {
			tmp, err = password.GetPassword()
		}
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}
	return tmp
}

func walletAction(context *cli.Context) {
	if context.NumFlags() == 0 {
		cli.ShowSubcommandHelp(context)
		os.Exit(0)
	}
	pass := context.String("password")

	// create wallet
	if context.Bool("create") {
		if err := createWallet([]byte(pass)); err != nil {
			fmt.Println("error: create wallet failed, ", err)
			cli.ShowCommandHelpAndExit(context, "create", 1)
		}
		return
	}

	wallet, err := walt.Open()
	if err != nil {
		fmt.Println("error: open wallet failed, ", err)
		os.Exit(2)
	}

	// show account info
	if context.Bool("account") {
		if err := showAccountInfo([]byte(pass)); err != nil {
			fmt.Println("error: show account info failed, ", err)
			cli.ShowCommandHelpAndExit(context, "account", 3)
		}
		return
	}

	// change password
	if context.Bool("changepassword") {
		if err := changePassword([]byte(pass), wallet); err != nil {
			fmt.Println("error: change password failed, ", err)
			cli.ShowCommandHelpAndExit(context, "changepassword", 4)
		}
		return
	}

	// add multisig account
	if pubKeysStr := context.String("addaccount"); pubKeysStr != "" {
		if err := addAccount(wallet, pubKeysStr); err != nil {
			fmt.Println("error: add multi sign account failed, ", err)
			cli.ShowCommandHelpAndExit(context, "addaccount", 5)
		}
		return
	}

	// show addresses balance in this wallet
	if context.Bool("balance") {
		if err := listBalanceInfo(wallet); err != nil {
			fmt.Println("error: list balance info failed, ", err)
			cli.ShowCommandHelpAndExit(context, "balance", 6)
		}
		return
	}

	// transaction actions
	if param := context.String("transaction"); param != "" {
		switch param {
		case "create":
			if err := createTransaction(context, wallet); err != nil {
				fmt.Println("error:", err)
				os.Exit(701)
			}
		case "sign":
			if err := signTransaction([]byte(pass), context, wallet); err != nil {
				fmt.Println("error:", err)
				os.Exit(702)
			}
		case "send":
			if err := sendTransaction(context); err != nil {
				fmt.Println("error:", err)
				os.Exit(703)
			}
		default:
			cli.ShowCommandHelpAndExit(context, "transaction", 700)
		}
		return
	}

	// reset wallet
	if context.Bool("reset") {
		if err := wallet.Reset(); err != nil {
			fmt.Println("error: reset wallet data store failed, ", err)
			cli.ShowCommandHelpAndExit(context, "reset", 8)
		}
		fmt.Println("wallet data store was reset successfully")
		return
	}
}

func NewCommand() *cli.Command {
	return &cli.Command{
		Name:        "wallet",
		Usage:       "user wallet operation",
		Description: "With ela-cli wallet, you could control your asset.",
		ArgsUsage:   "[args]",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "password, p",
				Usage: "keystore password",
			},
			cli.BoolFlag{
				Name:  "create, c",
				Usage: "create wallet",
			},
			cli.BoolFlag{
				Name:  "account, a",
				Usage: "show account info",
			},
			cli.BoolFlag{
				Name:  "changepassword",
				Usage: "change wallet password",
			},
			cli.StringFlag{
				Name: "addaccount",
				Usage: "add a standard account with it's public key" +
					", or add a multi-sign account using signers public keys",
			},
			cli.BoolFlag{
				Name:  "balance, b",
				Usage: "list balances",
			},
			cli.StringFlag{
				Name: "transaction, t",
				Usage: "use [create, sign, send], to create, sign or send a transaction\n" +
					"\tcreate:\n" +
					"\t\tuse [--from] --to --amount --fee [--lock], or [--from] --content --fee [--lock]\n" +
					"\t\tto create a standard transaction, or multi output transaction\n" +
					"\tsign, send:\n" +
					"\t\tuse --content to specify the transaction file path or it's content\n",
			},
			cli.StringFlag{
				Name:  "from",
				Usage: "the spend address of the transaction, if not specified use default address",
			},
			cli.StringFlag{
				Name:  "to",
				Usage: "the receive address of the transaction",
			},
			cli.StringFlag{
				Name:  "amount",
				Usage: "the transfer amount of the transaction",
			},
			cli.StringFlag{
				Name:  "fee",
				Usage: "the transfer fee of the transaction",
			},
			cli.StringFlag{
				Name:  "lock",
				Usage: "the lock time to specify when the received asset can be spent",
			},
			cli.StringFlag{
				Name: "content",
				Usage: "the file path to specify a CSV format file with [address,amount] as multi output content,\n" +
					"or the transaction file path or it's content to be sign or send",
			},
			cli.BoolFlag{
				Name:  "reset",
				Usage: "reset wallet data store",
			},
		},
		Action: walletAction,
		OnUsageError: func(c *cli.Context, err error, isSubcommand bool) error {
			PrintError(c, err, "wallet")
			return cli.NewExitError("", 1)
		},
	}
}
