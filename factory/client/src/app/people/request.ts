// What a section of this screen asks the screen to send. A screen's sections
// hold the rows and the forms; the screen holds the one path every call takes
// — refused while the subscription is down, re-reading the address first, and
// re-reading it again after — so a section emits the call it wants made and
// performs none itself.
//
// One copy per screen directory large enough to be split into sections —
// factory and people — identical in both. A screen imports api/ and state/
// and nothing else outside its own directory, and this repetition is what
// buys that: a defect found in one copy is found in the other by one search
// for the same name.
export interface CallRequest {
  // name is a method of package screens' Calls with a lowercased first
  // letter, which is the address ../../../../screens/call.go switches over.
  readonly name: string;
  readonly args: object;
}
