// What a section of this screen asks the screen to send. The four sections
// hold the rows and the forms; the screen holds the one path every call takes
// — refused while the subscription is down, re-reading the address first, and
// re-reading it again after — so a section emits the call it wants made and
// performs none itself.
//
// A copy of the same file under ../people/, which is the other screen large
// enough to be split into sections. A screen imports api/ and state/ and
// nothing else outside its own directory, and the copy is what that costs.
export interface CallRequest {
  // name is a method of package screens' Calls with a lowercased first
  // letter, which is the address ../../../../screens/call.go switches over.
  readonly name: string;
  readonly args: object;
}
