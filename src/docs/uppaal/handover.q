//no deadlock
A[] not deadlock

//T-RC7: a stored document is taken up within GRACE + RECONCILE
A[] (stored && !adopted) imply a <= GRACE + RECONCILE

//a lost message is made up by the reconcile pass
E<> (Message.Lost && Receiver.Adopted)

//a delivered message is adopted at once
E<> (Message.Delivered && Receiver.Adopted)
