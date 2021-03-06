package models

import (
	"database/sql"
	"time"
)

const (
	// RegistrationChallengeCount is the number of registration challenges
	// required to be completed before a user is considered registered.
	RegistrationChallengeCount = 3
)

// RegistrationChallenge is a mapping between users and registration questions,
// facilitating the registration process
type RegistrationChallenge struct {
	ID                     int64      `db:"id"`
	AccountID              int64      `db:"account_id"`
	RegistrationQuestionID int64      `db:"registration_question_id"`
	SentAt                 *time.Time `db:"sent_at"`
	CompletedAt            *time.Time `db:"completed_at"`
}

// RegistrationChallengeRegistrationQuestion is a union between challenges and
// questions to facilitate joins between the two tables.
type RegistrationChallengeRegistrationQuestion struct {
	RegistrationChallenge
	RegistrationQuestion `db:"registration_questions"`
}

// CreateRegistrationChallenge stores a new challenge in the database
func CreateRegistrationChallenge(account *Account, question *RegistrationQuestion) (*RegistrationChallenge, error) {
	db := GetDBSession()

	challenge := &RegistrationChallenge{
		AccountID:              account.ID,
		RegistrationQuestionID: question.ID,
	}

	var id int64
	err := db.QueryRow(`
		INSERT INTO registration_challenges (
			account_id,
			registration_question_id
		) VALUES($1, $2)
		RETURNING id
	`, account.ID, question.ID).Scan(&id)

	if err != nil {
		return nil, err
	}

	challenge.ID = id

	return challenge, nil
}

func (challenge *RegistrationChallenge) MarkSent() error {
	db := GetDBSession()

	now := time.Now().UTC()
	_, err := db.Exec(`
		UPDATE registration_challenges
			SET sent_at = $1
		WHERE id = $2
	`, &now, challenge.ID)

	return err
}

func (challenge *RegistrationChallenge) MarkCompleted() error {
	db := GetDBSession()

	now := time.Now().UTC()
	_, err := db.Exec(`
		UPDATE registration_challenges
		SET completed_at = $1
		WHERE id = $2
	`, &now, challenge.ID)

	return err
}

func FindUnsentChallenge(accountId int64) (*RegistrationChallengeRegistrationQuestion, error) {
	db := GetDBSession()

	challenge := &RegistrationChallengeRegistrationQuestion{}
	err := db.Get(challenge, `
		SELECT
			registration_challenges.*,
			registration_questions.id "registration_questions.id",
			registration_questions.question "registration_questions.question",
			registration_questions.answer "registration_questions.answer"
		FROM registration_challenges
		JOIN registration_questions ON
			registration_challenges.registration_question_id = registration_questions.id
		WHERE
			account_id = $1 AND
			sent_at IS NULL
		ORDER BY RANDOM()
		LIMIT 1
	`, accountId)

	if err != nil && err != sql.ErrNoRows {
		return nil, err
	} else if err == sql.ErrNoRows {
		return nil, nil
	}

	return challenge, nil
}

func FindIncompleteChallenge(accountId int64) (*RegistrationChallengeRegistrationQuestion, error) {
	db := GetDBSession()

	challenge := &RegistrationChallengeRegistrationQuestion{}
	err := db.Get(challenge, `
		SELECT
			registration_challenges.*,
			registration_questions.id "registration_questions.id",
			registration_questions.question "registration_questions.question",
			registration_questions.answer "registration_questions.answer"
		FROM registration_challenges
		JOIN registration_questions ON
			registration_challenges.registration_question_id = registration_questions.id
		WHERE
			account_id = $1 AND
			sent_at IS NOT NULL AND
			completed_at IS NULL
		LIMIT 1
	`, accountId)

	if err != nil && err != sql.ErrNoRows {
		return nil, err
	} else if err == sql.ErrNoRows {
		return nil, nil
	}

	return challenge, nil
}
