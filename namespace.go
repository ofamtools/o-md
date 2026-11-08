package main

import "ofam.dev/ochazuke"

// Namespace is the singular namespace o-md declares (lexicon plan): its
// operations are md.<noun>.<verb> and a host mounts its catalog under it.
const Namespace = "md"

func init() {
	if err := ochazuke.DeclareNamespace(Namespace); err != nil {
		panic(err)
	}
}
