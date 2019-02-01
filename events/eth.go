package events

import (
	"fmt"
	"log"
	"math/big"
	"os"

	"gitlab.com/alpinefresh/tcrpartybot/contracts"
	"gitlab.com/alpinefresh/tcrpartybot/models"
	"gitlab.com/alpinefresh/tcrpartybot/twitter"
)

const (
	newApplicationWithHandleTweet    = "New #TCRParty listing! @%s has nominated @%s to be on the list for %s TCRP. Challenge this application by DMing 'challenge @%s'."
	newApplicationWithoutHandleTweet = "New #TCRParty listing! @%s has been nominated to be on the list for %s TCRP. Challenge this application by DMing 'challenge @%s'."
	newChallengeTweet                = "New #TCRParty challenge! @%s's listing has been put to the test. Send me a DM with 'vote %s keep/kick' to determine their fate."
	applicationWhitelistedTweet      = "@%s has been successfully added to the #TCRParty!"
	applicationRemovedTweet          = "@%s has been removed from the #TCRParty."
	challengeSucceededTweet          = "The challenge against @%s's listing succeeded! They're out of the party."
	challengeFailedTweet             = "The challenge against @%s's listing failed! Their spot in the party remains."
	walletConfirmedMsg               = "Done! Your wallet is good to go and has %d TCRP waiting for you. Try responding with 'help' to see what you can ask me to do."

	withdrawalMsg = "The challenge against your listing for %s failed! As a result you've won %d tokens. Your new balance is %d"

	minDepositAmount   = 500
	initialTokenAmount = 1550
	initialVoteAmount  = 50
)

func processMultisigWalletCreation(event *ETHEvent) error {
	instantiation, err := contracts.DecodeContractInstantiationEvent(event.Data)
	if err != nil {
		return err
	}

	account, err := models.FindAccountByMultisigFactoryIdentifier(instantiation.Identifier.Int64())
	if err != nil {
		return err
	} else if account == nil {
		log.Printf("Could not find account with identifier %d", instantiation.Identifier.Int64())
		return nil
	}

	// Link their newly created multisig address to the account
	multisigAddress := instantiation.Instantiation.Hex()
	err = account.SetMultisigAddress(multisigAddress)
	if err != nil {
		return err
	}

	log.Printf("Wallet at %s linked to %s\n", multisigAddress, account.TwitterHandle)

	// Mint them 50 tokens for voting
	atomicAmount := contracts.GetAtomicTokenAmount(initialTokenAmount)
	mintTx, err := contracts.MintTokens(multisigAddress, atomicAmount)
	if err != nil {
		return err
	}

	_, err = contracts.AwaitTransactionConfirmation(mintTx.Hash())
	if err != nil {
		return err
	}

	// Lock some tokens up into the voting contract
	atomicAmount = contracts.GetAtomicTokenAmount(initialVoteAmount)
	plcrTX, err := contracts.PLCRDeposit(multisigAddress, atomicAmount)
	if err != nil {
		return err
	}

	_, err = contracts.AwaitTransactionConfirmation(plcrTX.Hash())
	if err != nil {
		return err
	}

	if os.Getenv("PREREGISTRATION") != "true" {
		balance, err := contracts.GetTokenBalance(multisigAddress)
		if err != nil {
			return err
		}

		humanBalance := contracts.GetHumanTokenAmount(balance)
		msg := fmt.Sprintf(walletConfirmedMsg, humanBalance)
		twitter.SendDM(account.TwitterID, msg)
	}

	return nil
}

func processWithdrawal(event *ETHEvent) error {
	withdrawal, err := contracts.DecodeWithdrawalEvent(event.Topics, event.Data)
	if err != nil {
		return err
	}

	account, err := models.FindAccountByMultisigAddress(withdrawal.Owner.Hex())
	if err != nil {
		return err
	} else if account == nil {
		log.Printf("Withdrawal from unkown owner %s", withdrawal.Owner.Hex())
		return nil
	}

	humanReward := contracts.GetHumanTokenAmount(withdrawal.Withdrew)

	// Get the listing's handle
	listingHandle, err := contracts.GetListingDataFromHash(withdrawal.ListingHash)
	if err != nil {
		return err
	}

	// Get their wallet balance
	balance, err := contracts.GetTokenBalance(account.MultisigAddress.String)
	if err != nil {
		return err
	}
	humanBalance := contracts.GetHumanTokenAmount(balance).Int64()

	// Send the owner a notification
	msg := fmt.Sprintf(withdrawalMsg, listingHandle, humanReward, humanBalance)
	err = twitter.SendDM(account.TwitterID, msg)
	return err
}

func processNewApplication(event *ETHEvent) error {
	application, err := contracts.DecodeApplicationEvent(event.Topics, event.Data)
	if err != nil {
		return err
	}

	// See if we can find an applicant in our database
	log.Printf("New application from %s for %s (hash: 0x%x)", application.Applicant.Hex(), application.Data, application.ListingHash)
	account, err := models.FindAccountByMultisigAddress(application.Applicant.Hex())
	if err != nil {
		return err
	}

	tweet := ""
	depositAmount := contracts.GetHumanTokenAmount(application.Deposit).String()
	if account != nil {
		tweet = fmt.Sprintf(
			newApplicationWithHandleTweet,
			account.TwitterHandle,
			application.Data,
			depositAmount,
			application.Data,
		)
	} else {
		tweet = fmt.Sprintf(
			newApplicationWithoutHandleTweet,
			application.Data,
			depositAmount,
			application.Data,
		)
	}

	return twitter.SendTweet(twitter.VIPBotHandle, tweet)
}

func processNewChallenge(event *ETHEvent) error {
	challenge, err := contracts.DecodeChallengeEvent(event.Topics, event.Data)
	if err != nil {
		return err
	}

	listing, err := contracts.GetListingFromHash(challenge.ListingHash)
	if err != nil {
		return err
	} else if listing == nil {
		return fmt.Errorf("Could not find listing for challenge %s (listing: %s)", challenge.ChallengeID, string(challenge.ListingHash[:]))
	}

	log.Printf("New challenge for %s (hash: 0x%x)", challenge.Data, challenge.ListingHash)

	tweet := fmt.Sprintf(
		newChallengeTweet,
		challenge.Data,
		challenge.Data,
	)

	return twitter.SendTweet(twitter.VIPBotHandle, tweet)
}

func processApplicationWhitelisted(ethEvent *ETHEvent) error {
	event, err := contracts.DecodeApplicationWhitelistedEvent(ethEvent.Topics, ethEvent.Data)
	if err != nil {
		return err
	}

	data, err := contracts.GetListingDataFromHash(event.ListingHash)
	if err != nil {
		return err
	}

	log.Printf("Application for %s whitelisted!", data)
	tweet := fmt.Sprintf(applicationWhitelistedTweet, data)

	return twitter.SendTweet(twitter.VIPBotHandle, tweet)
}

func processChallengeSucceeded(ethEvent *ETHEvent) error {
	event, err := contracts.DecodeChallengeSucceededEvent(ethEvent.Topics, ethEvent.Data)
	if err != nil {
		return err
	}

	data, err := contracts.GetListingDataFromHash(event.ListingHash)
	if err != nil {
		return err
	}

	log.Printf("Challenge against %s succeeded!", data)
	tweet := fmt.Sprintf(challengeSucceededTweet, data)

	return twitter.SendTweet(twitter.VIPBotHandle, tweet)
}

func processChallengeFailed(ethEvent *ETHEvent) error {
	event, err := contracts.DecodeChallengeFailedEvent(ethEvent.Topics, ethEvent.Data)
	if err != nil {
		return err
	}

	data, err := contracts.GetListingDataFromHash(event.ListingHash)
	if err != nil {
		return err
	}

	log.Printf("Challenge against %s failed!", data)
	tweet := fmt.Sprintf(challengeFailedTweet, data)

	// Fetch how many tokens the listing owner receives
	listing, err := contracts.GetListingFromHash(event.ListingHash)
	if err != nil {
		return err
	}

	unstaked := listing.UnstakedDeposit
	if unstaked.Cmp(big.NewInt(0)) == 1 {
		log.Printf("Owner has unstaked tokens available, unlocking...")
		// Unlock tokens and send them to the owner
		reward := unstaked.Sub(unstaked, contracts.GetAtomicTokenAmount(minDepositAmount))
		if _, err = contracts.Withdraw(data, reward); err != nil {
			return err
		}
	}

	return twitter.SendTweet(twitter.VIPBotHandle, tweet)
}

func processApplicationRemoved(ethEvent *ETHEvent) error {
	event, err := contracts.DecodeApplicationRemovedEvent(ethEvent.Topics, ethEvent.Data)
	if err != nil {
		return err
	}

	data, err := contracts.GetListingDataFromHash(event.ListingHash)
	if err != nil {
		return err
	}

	log.Printf("Application @%s removed!", data)
	tweet := fmt.Sprintf(applicationRemovedTweet, data)

	return twitter.SendTweet(twitter.VIPBotHandle, tweet)
}
