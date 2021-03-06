// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

/*
Package common - helper functions that enable service to get and set encrypted envelopes
*/
package common

import (
	"encoding/hex"
	"io"
	"time"

	"github.com/apache/incubator-milagro-dta/libs/datastore"
	"github.com/apache/incubator-milagro-dta/libs/documents"
	"github.com/apache/incubator-milagro-dta/libs/ipfs"
	"github.com/apache/incubator-milagro-dta/libs/keystore"
	"github.com/apache/incubator-milagro-dta/pkg/identity"
	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// CreateNewDepositOrder - Generate an empty new Deposit Order with random reference
func CreateNewDepositOrder(BeneficiaryIDDocumentCID string, nodeID string) (*documents.OrderDoc, error) {
	//Create a reference for this order
	reference, err := uuid.NewUUID()
	if err != nil {
		return nil, err
	}
	order := documents.NewOrderDoc()
	//oder.Type will be used to extend the things that an order can do.
	order.Type = "Safeguard_Secret"
	order.PrincipalCID = nodeID
	order.Reference = reference.String()
	order.BeneficiaryCID = BeneficiaryIDDocumentCID
	order.Timestamp = time.Now().Unix()
	return &order, nil
}

// RetrieveOrderFromIPFS - retrieve an Order from IPFS and Decode the into an  object
func RetrieveOrderFromIPFS(ipfs ipfs.Connector, ipfsID string, sikeSK []byte, recipientID string, sendersBlsPK []byte) (*documents.OrderDoc, error) {
	o := &documents.OrderDoc{}
	rawDocO, err := ipfs.Get(ipfsID)
	if err != nil {
		return nil, err
	}
	err = documents.DecodeOrderDocument(rawDocO, ipfsID, o, sikeSK, recipientID, sendersBlsPK)
	return o, err
}

// RetrieveIDDocFromIPFS finds and parses the IDDocument
func RetrieveIDDocFromIPFS(ipfs ipfs.Connector, ipfsID string) (*documents.IDDoc, error) {
	iddoc := &documents.IDDoc{}
	rawDocI, err := ipfs.Get(ipfsID)
	if err != nil {
		return nil, err
	}
	err = documents.DecodeIDDocument(rawDocI, ipfsID, iddoc)
	return iddoc, err
}

// MakeRandomSeedAndStore genefates and stores a random seed
func MakeRandomSeedAndStore(store *datastore.Store, rng io.Reader, reference string) (seedHex string, err error) {
	seed := make([]byte, 32)
	if _, err := io.ReadFull(rng, seed); err != nil {
		return "", err
	}
	i := len(seed)
	if i > 32 {
		i = 32
	}
	var byte32 [32]byte
	copy(byte32[:], seed[:i])
	seedHex = hex.EncodeToString(seed)
	if err := store.Set("keySeed", reference, seedHex, nil); err != nil {
		return "", errors.Wrap(err, "store seed")
	}
	return seedHex, nil
}

// RetrieveSeed gets the seed from the key store
func RetrieveSeed(store *datastore.Store, reference string) (seedHex string, err error) {
	if err := store.Get("keySeed", reference, &seedHex); err != nil {
		return "", nil
	}
	return seedHex, nil
}

// CreateAndStoreOrderPart2 -
func CreateAndStoreOrderPart2(ipfs ipfs.Connector, store *datastore.Store, keyStore keystore.Store, order *documents.OrderDoc, orderPart1CID, commitmentPublicKey, nodeID string, recipients map[string]*documents.IDDoc) (orderPart2CID string, err error) {
	Part2 := documents.OrderPart2{
		CommitmentPublicKey: commitmentPublicKey,
		PreviousOrderCID:    orderPart1CID,
		Timestamp:           time.Now().Unix(),
	}
	order.OrderPart2 = &Part2
	//Write the updated doc back to IPFS
	orderPart2CID, err = WriteOrderToIPFS(nodeID, ipfs, store, keyStore, nodeID, order, recipients)
	if err != nil {
		return "", err
	}
	return orderPart2CID, nil
}

// CreateAndStorePart3 adds part 3 "redemption request" to the order doc
func CreateAndStorePart3(ipfs ipfs.Connector, store *datastore.Store, keyStore keystore.Store, order *documents.OrderDoc, orderPart2CID, nodeID string, beneficiaryEncryptedData []byte, recipients map[string]*documents.IDDoc) (orderPart3CID string, err error) {
	//Add part 3 "redemption request" to the order doc
	redemptionRequest := documents.OrderPart3{
		//TODO
		Redemption:               "SignedReferenceNumber",
		PreviousOrderCID:         orderPart2CID,
		BeneficiaryEncryptedData: beneficiaryEncryptedData,
		Timestamp:                time.Now().Unix(),
	}
	order.OrderPart3 = &redemptionRequest
	//Write the updated doc back to IPFS
	orderPart3CID, err = WriteOrderToIPFS(nodeID, ipfs, store, keyStore, nodeID, order, recipients)
	if err != nil {
		return "", nil
	}
	return orderPart3CID, nil
}

// CreateAndStoreOrderPart4 -
func CreateAndStoreOrderPart4(ipfs ipfs.Connector, store *datastore.Store, keyStore keystore.Store, order *documents.OrderDoc, commitmentPrivateKey, orderPart3CID, nodeID string, recipients map[string]*documents.IDDoc) (orderPart4CID string, err error) {
	Part4 := documents.OrderPart4{
		Secret:           commitmentPrivateKey,
		PreviousOrderCID: orderPart3CID,
		Timestamp:        time.Now().Unix(),
	}
	order.OrderPart4 = &Part4
	//Write the updated doc back to IPFS
	orderPart4CID, err = WriteOrderToIPFS(nodeID, ipfs, store, keyStore, nodeID, order, recipients)
	if err != nil {
		return "", nil
	}
	return orderPart4CID, nil
}

// WriteOrderToIPFS writes the order document to IPFS network
func WriteOrderToIPFS(nodeID string, ipfs ipfs.Connector, store *datastore.Store, keyStore keystore.Store, id string, order *documents.OrderDoc, recipients map[string]*documents.IDDoc) (ipfsAddress string, err error) { // Get the secret keys
	seed, err := keyStore.Get("seed")
	if err != nil {
		return "", errors.New("load secrets")
	}
	_, blsSecretKey, err := identity.GenerateBLSKeys(seed)
	if err != nil {
		return "", err
	}

	rawDoc, err := documents.EncodeOrderDocument(nodeID, *order, blsSecretKey, recipients)
	if err != nil {
		return "", errors.Wrap(err, "Failed to encode IDDocument")
	}
	ipfsAddress, err = ipfs.Add(rawDoc)
	if err != nil {
		return "", errors.Wrap(err, "Failed to Save Raw Document into IPFS")
	}

	if err := store.Set("order", order.Reference, ipfsAddress, map[string]string{"time": time.Now().UTC().Format(time.RFC3339)}); err != nil {
		return "", errors.New("Save Order to store")
	}
	return ipfsAddress, nil
}

// BuildRecipientList builds a list of recipients who are able to decrypt the encrypted envelope
func BuildRecipientList(ipfs ipfs.Connector, localNodeDocCID, remoteNodeDocCID string) (map[string]*documents.IDDoc, error) {
	remoteNodeDoc, err := RetrieveIDDocFromIPFS(ipfs, remoteNodeDocCID)
	if err != nil {
		return nil, err
	}

	localNodeDoc, err := RetrieveIDDocFromIPFS(ipfs, localNodeDocCID)
	if err != nil {
		return nil, err
	}

	recipients := map[string]*documents.IDDoc{
		remoteNodeDocCID: remoteNodeDoc,
		localNodeDocCID:  localNodeDoc,
	}
	return recipients, nil
}
