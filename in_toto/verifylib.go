package main

import (
  "fmt"
  // "reflect"
)

// func VerifyLayoutSignatures() {

// }


func InTotoVerify() {
  var link_write_code Metablock
  var link_package Metablock
  var layout Metablock
  var pubKey Key

  layout.Load("../test/data/demo.layout.template")
  // fmt.Println(layout.Signed.(Layout).Keys["2f89b9272acfc8f4a0a0f094d789fdb0ba798b0fe41f2f5f417c12f0085ff498"])
  link_write_code.Load("../test/data/write-code.776a00e2.link")
  link_package.Load("../test/data/package.2f89b927.link")


  // fmt.Println(reflect.TypeOf(link_package.Signed.(Link).ByProducts["return-value"]))

  // Works :)
  link_write_code.VerifySignature(
      layout.Signed.(Layout).Keys["776a00e29f3559e0141b3b096f696abc6cfb0c657ab40f441132b345b08453f5"])

  // Fails :'(
  // The reason is that `package.2f89b927.link` has a "\n" in it's stderr,
  // which in Python returns one byte ('0a') and in go two bytes (5c and 6e).
  // FIXME: tell the go json decoder to not escape the control sequence
  link_package.VerifySignature(
      layout.Signed.(Layout).Keys["2f89b9272acfc8f4a0a0f094d789fdb0ba798b0fe41f2f5f417c12f0085ff498"])


  pubKey.LoadPublicKey("/Users/lukp/go/src/github.com/in-toto/in-toto-golang/test/data/alice.pub")
  // fmt.Printf("%s", encode_canonical(pubKey))


  layout.VerifySignature(pubKey)

  fmt.Println("")

}



// TODO: This should go to ROOT/cmd and become a command line interface that
// calls verifylib.InTotoVerify with the right parameters
func main() {
  InTotoVerify()
}