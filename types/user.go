package types

type User struct {
	Id        int    `db:"id" json:"id"`
	FirstName string `db:"firstname" json:"firstName"`
	LastName  string `db:"lastname" json:"lastName"`
	Age       int    `db:"age" json:"age"`
}

type CreateUserDto struct {
	FirstName string `db:"firstname" json:"firstName"`
	LastName  string `db:"lastname" json:"lastName"`
	Age       int    `db:"age" json:"age"`
}
