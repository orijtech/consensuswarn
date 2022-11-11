package testdata

func RootFunc1() {
	StateFunc1()
}

/*



Space the separate hunks



*/
func StateFunc1() {
	println("state change!2")
}

/*




Space to separate hunks.




*/
func NonStateFunc1() {
}

/*


More space.


*/
type T struct{}

func (t *T) RootMethod1() {
	t.StateMethod1()
}

func (t *T) StateMethod1() {
}

/*



More space.



*/
func (*T) NonStateMethod1() {
}
