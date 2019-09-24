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

package defaultservice

import (
	"encoding/json"
	"time"

	"github.com/apache/incubator-milagro-dta/libs/cryptowallet"
	"github.com/apache/incubator-milagro-dta/libs/documents"
	"github.com/apache/incubator-milagro-dta/pkg/api"
	"github.com/apache/incubator-milagro-dta/pkg/common"
	"github.com/apache/incubator-milagro-dta/pkg/tendermint"
	"github.com/pkg/errors"
)

// GetOrder retreives an order
func (s *Service) GetOrder(req *api.GetOrderRequest) (*api.GetOrderResponse, error) {
	orderReference := req.OrderReference

	var cid string
	if err := s.Store.Get("order", orderReference, &cid); err != nil {
		return nil, err
	}

	localIDDoc, err := common.RetrieveIDDocFromIPFS(s.Ipfs, s.NodeID())
	if err != nil {
		return nil, err
	}

	_, _, _, sikeSK, err := common.RetrieveIdentitySecrets(s.Store, s.NodeID())
	if err != nil {
		return nil, err
	}

	order, err := common.RetrieveOrderFromIPFS(s.Ipfs, cid, sikeSK, s.NodeID(), localIDDoc.BLSPublicKey)
	if err != nil {
		return nil, err
	}

	orderByte, err := json.Marshal(order)
	if err != nil {
		return nil, err
	}
	return &api.GetOrderResponse{
		OrderCID: cid,
		Order:    string(orderByte),
	}, nil
}

// OrderList retrieves the list of orders
func (s *Service) OrderList(req *api.OrderListRequest) (*api.OrderListResponse, error) {
	page := req.Page
	perPage := req.PerPage
	sortBy := req.SortBy

	orderref, err := s.Store.ListKeys("order", "time", page*perPage, perPage, sortBy != "dateCreatedAsc")
	if err != nil {
		return nil, err
	}

	//Pagnination - Show everything by default
	start := 0
	stop := len(orderref)

	if perPage != 0 && page < len(orderref)/perPage && page*perPage < len(orderref) {
		start = page
		stop = perPage
	}

	return &api.OrderListResponse{
		OrderReference: orderref[start:stop],
	}, nil
}

// ValidateOrderRequest returns error if the request values are invalid
func (s *Service) ValidateOrderRequest(req *api.OrderRequest) error {
	return nil
}

//ValidateOrderSecretRequest - Validate fields in the Order Secret
func (s *Service) ValidateOrderSecretRequest(req *api.OrderSecretRequest, order documents.OrderDoc) error {
	return nil
}

// PrepareOrderPart1 is called before the order is send
func (s *Service) PrepareOrderPart1(order *documents.OrderDoc, reqExtension map[string]string) (fulfillExtension map[string]string, err error) {
	return nil, nil
}

// PrepareOrderResponse gets the updated order and returns the commitment and extension
func (s *Service) PrepareOrderResponse(orderPart2 *documents.OrderDoc, reqExtension, fulfillExtension map[string]string) (commitment string, extension map[string]string, err error) {
	return orderPart2.OrderPart2.CommitmentPublicKey, nil, nil
}

// Order -
func (s *Service) Order(req *api.OrderRequest) (*api.OrderResponse, error) {
	if err := s.Plugin.ValidateOrderRequest(req); err != nil {
		return nil, err
	}

	//Initialise values from Request object
	beneficiaryIDDocumentCID := req.BeneficiaryIDDocumentCID
	nodeID := s.NodeID()
	recipientList, err := common.BuildRecipientList(s.Ipfs, nodeID, s.MasterFiduciaryNodeID())
	if err != nil {
		return nil, err
	}

	remoteIDDoc, err := common.RetrieveIDDocFromIPFS(s.Ipfs, s.MasterFiduciaryNodeID())
	if err != nil {
		return nil, err
	}

	//Create Order
	order, err := common.CreateNewDepositOrder(beneficiaryIDDocumentCID, nodeID)
	if err != nil {
		return nil, err
	}

	fulfillExtension, err := s.Plugin.PrepareOrderPart1(order, req.Extension)
	if err != nil {
		return nil, err
	}

	//Write Order to IPFS
	orderPart1CID, err := common.WriteOrderToIPFS(nodeID, s.Ipfs, s.Store, nodeID, order, recipientList)
	if err != nil {
		return nil, err
	}

	//Fullfill the order on the remote Server
	request := &api.FulfillOrderRequest{
		DocumentCID:   nodeID,
		OrderPart1CID: orderPart1CID,
		Extension:     fulfillExtension,
	}

	response, err := s.MasterFiduciaryServer.FulfillOrder(request)
	if err != nil {
		return nil, errors.Wrap(err, "Contacting Fiduciary")
	}

	//Get the updated order out of IPFS
	_, _, _, sikeSK, err := common.RetrieveIdentitySecrets(s.Store, nodeID)
	if err != nil {
		return nil, err
	}
	updatedOrder, err := common.RetrieveOrderFromIPFS(s.Ipfs, response.OrderPart2CID, sikeSK, nodeID, remoteIDDoc.BLSPublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "Fail to retrieve Order from IPFS")
	}

	commitment, extension, err := s.Plugin.PrepareOrderResponse(updatedOrder, req.Extension, response.Extension)
	if err != nil {
		return nil, errors.Wrap(err, "Generating Final Public Key")
	}

	return &api.OrderResponse{
		OrderReference: order.Reference,
		Commitment:     commitment,
		CreatedAt:      time.Now().Unix(),
		Extension:      extension,
	}, nil
}

// ProduceBeneficiaryEncryptedData -
func (s *Service) ProduceBeneficiaryEncryptedData(blsSK []byte, order *documents.OrderDoc, req *api.OrderSecretRequest) (encrypted []byte, extension map[string]string, err error) {
	return nil, nil, nil
}

// ProduceFinalSecret -
func (s *Service) ProduceFinalSecret(seed, sikeSK []byte, order, orderPart4 *documents.OrderDoc, beneficiaryIDDocumentCID string) (secret, commitment string, extension map[string]string, err error) {
	finalPrivateKey := orderPart4.OrderDocument.OrderPart4.Secret
	//Derive the Public key from the supplied Private Key
	finalPublicKey, _, err := cryptowallet.PublicKeyFromPrivate(finalPrivateKey)
	return finalPrivateKey, finalPublicKey, nil, err
}

// OrderSecret -
func (s *Service) OrderSecret(req *api.OrderSecretRequest) (*api.OrderSecretResponse, error) {
	orderReference := req.OrderReference
	var orderPart2CID string
	if err := s.Store.Get("order", orderReference, &orderPart2CID); err != nil {
		return nil, err
	}

	nodeID := s.NodeID()
	recipientList, err := common.BuildRecipientList(s.Ipfs, nodeID, s.MasterFiduciaryNodeID())
	if err != nil {
		return nil, err
	}
	remoteIDDoc, err := common.RetrieveIDDocFromIPFS(s.Ipfs, s.MasterFiduciaryNodeID())
	if err != nil {
		return nil, err
	}

	_, _, blsSK, sikeSK, err := common.RetrieveIdentitySecrets(s.Store, nodeID)
	if err != nil {
		return nil, err
	}

	//Retrieve the order from IPFS

	order, err := common.RetrieveOrderFromIPFS(s.Ipfs, orderPart2CID, sikeSK, nodeID, remoteIDDoc.BLSPublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "Fail to retrieve Order from IPFS")
	}

	if err := s.Plugin.ValidateOrderSecretRequest(req, *order); err != nil {
		return nil, err
	}

	var beneficiariesSikeSK []byte
	var beneficiaryCID string

	if req.BeneficiaryIDDocumentCID != "" {
		beneficiaryCID = req.BeneficiaryIDDocumentCID
	} else {
		beneficiaryCID = order.BeneficiaryCID
	}

	_, beneficiariesSeed, _, beneficiariesSikeSK, err := common.RetrieveIdentitySecrets(s.Store, beneficiaryCID)
	if err != nil {
		return nil, err
	}

	//Create a piece of data that is destined for the beneficiary, passed via the Master Fiduciary

	beneficiaryEncryptedData, extension, err := s.Plugin.ProduceBeneficiaryEncryptedData(blsSK, order, req)
	if err != nil {
		return nil, err
	}

	//Create a request Object in IPFS
	orderPart3CID, err := common.CreateAndStorePart3(s.Ipfs, s.Store, order, orderPart2CID, nodeID, beneficiaryEncryptedData, recipientList)
	if err != nil {
		return nil, err
	}

	//Post the address of the updated doc to the custody node
	request := &api.FulfillOrderSecretRequest{
		SenderDocumentCID: nodeID,
		OrderPart3CID:     orderPart3CID,
		Extension:         extension,
	}
	response, err := s.MasterFiduciaryServer.FulfillOrderSecret(request)
	if err != nil {
		return nil, err
	}

	//Retrieve the response Order from IPFS
	orderPart4, err := common.RetrieveOrderFromIPFS(s.Ipfs, response.OrderPart4CID, sikeSK, nodeID, remoteIDDoc.BLSPublicKey)
	if err != nil {
		return nil, err
	}

	finalPrivateKey, finalPublicKey, ext, err := s.Plugin.ProduceFinalSecret(beneficiariesSeed, beneficiariesSikeSK, order, orderPart4, beneficiaryCID)
	if err != nil {
		return nil, err
	}

	return &api.OrderSecretResponse{
		Secret:         finalPrivateKey,
		Commitment:     finalPublicKey,
		OrderReference: order.Reference,
		Extension:      ext,
	}, nil
}

// Order1 -
func (s *Service) Order1(req *api.OrderRequest) (string, error) {
	if err := s.Plugin.ValidateOrderRequest(req); err != nil {
		return "", err
	}

	//Initialise values from Request object
	beneficiaryIDDocumentCID := req.BeneficiaryIDDocumentCID
	nodeID := s.NodeID()
	recipientList, err := common.BuildRecipientList(s.Ipfs, nodeID, s.MasterFiduciaryNodeID())
	if err != nil {
		return "", err
	}

	// remoteIDDoc, err := common.RetrieveIDDocFromIPFS(s.Ipfs, s.MasterFiduciaryNodeID())
	// if err != nil {
	// 	return "", err
	// }

	//Create Order
	order, err := common.CreateNewDepositOrder(beneficiaryIDDocumentCID, nodeID)
	if err != nil {
		return "", err
	}

	fulfillExtension, err := s.Plugin.PrepareOrderPart1(order, req.Extension)
	if err != nil {
		return "", err
	}

	//Write Order to IPFS
	orderPart1CID, err := common.WriteOrderToIPFS(nodeID, s.Ipfs, s.Store, nodeID, order, recipientList)
	if err != nil {
		return "", err
	}

	//Fullfill the order on the remote Server
	request := &api.FulfillOrderRequest{
		DocumentCID:   nodeID,
		OrderPart1CID: orderPart1CID,
		Extension:     fulfillExtension,
	}

	marshaledRequest, _ := json.Marshal(request)

	//Write the requests to the chain
	chainTX := &api.BlockChainTX{
		Processor:   api.TXFulfillRequest,
		SenderID:    nodeID,
		RecipientID: []string{s.MasterFiduciaryNodeID(), nodeID},
		Payload:     marshaledRequest,
	}
	//curl --data-binary '{"jsonrpc":"2.0","id":"anything","method":"broadcast_tx_commit","params": {"tx": "YWFhcT1hYWFxCg=="}}' -H 'content-type:text/plain;' http://localhost:26657

	tendermint.PostToChain(chainTX, "Order1")
	return order.Reference, nil
}

// Order2 -
func (s *Service) Order2(req *api.FulfillOrderResponse) (string, error) {
	// if err := s.Plugin.ValidateOrderRequest(req); err != nil {
	// 	return "", err
	// }

	// //Initialise values from Request object
	// beneficiaryIDDocumentCID := req.BeneficiaryIDDocumentCID
	nodeID := s.NodeID()
	// recipientList, err := common.BuildRecipientList(s.Ipfs, nodeID, s.MasterFiduciaryNodeID())
	// if err != nil {
	// 	return "", err
	// }

	remoteIDDoc, err := common.RetrieveIDDocFromIPFS(s.Ipfs, s.MasterFiduciaryNodeID())
	if err != nil {
		return "", err
	}

	// //Create Order
	// order, err := common.CreateNewDepositOrder(beneficiaryIDDocumentCID, nodeID)
	// if err != nil {
	// 	return "", err
	// }

	// fulfillExtension, err := s.Plugin.PrepareOrderPart1(order, req.Extension)
	// if err != nil {
	// 	return "", err
	// }

	// //Write Order to IPFS
	// orderPart1CID, err := common.WriteOrderToIPFS(nodeID, s.Ipfs, s.Store, nodeID, order, recipientList)
	// if err != nil {
	// 	return "", err
	// }

	// //Fullfill the order on the remote Server
	// request := &api.FulfillOrderRequest{
	// 	DocumentCID:   nodeID,
	// 	OrderPart1CID: orderPart1CID,
	// 	Extension:     fulfillExtension,
	// }

	// marshaledRequest, _ := json.Marshal(request)

	// //Write the requests to the chain
	// chainTX := &api.BlockChainTX{
	// 	Type:        api.TXFullfullRequest,
	// 	SenderID:    nodeID,
	// 	RecipientID: s.MasterFiduciaryNodeID(),
	// 	Payload:     marshaledRequest,
	// }
	// //curl --data-binary '{"jsonrpc":"2.0","id":"anything","method":"broadcast_tx_commit","params": {"tx": "YWFhcT1hYWFxCg=="}}' -H 'content-type:text/plain;' http://localhost:26657

	// tendermint.PostToChain(chainTX)

	//  response, err := s.MasterFiduciaryServer.FulfillOrder(request)
	//  if err != nil {
	//  	return "", errors.Wrap(err, "Contacting Fiduciary")
	//  }

	//Get the updated order out of IPFS
	_, _, _, sikeSK, err := common.RetrieveIdentitySecrets(s.Store, nodeID)
	if err != nil {
		return "", err
	}
	updatedOrder, err := common.RetrieveOrderFromIPFS(s.Ipfs, req.OrderPart2CID, sikeSK, nodeID, remoteIDDoc.BLSPublicKey)
	if err != nil {
		return "", errors.Wrap(err, "Fail to retrieve Order from IPFS")
	}

	commitment, extension, err := s.Plugin.PrepareOrderResponse(updatedOrder, req.Extension, req.Extension)
	if err != nil {
		return "", errors.Wrap(err, "Generating Final Public Key")
	}

	response := &api.OrderResponse{
		OrderReference: updatedOrder.Reference,
		Commitment:     commitment,
		CreatedAt:      time.Now().Unix(),
		Extension:      extension,
	}

	marshaledRequest, _ := json.Marshal(response)

	//Write the requests to the chain
	chainTX := &api.BlockChainTX{
		Processor:   api.TXOrderSecretResponse,
		SenderID:    nodeID,
		RecipientID: []string{s.MasterFiduciaryNodeID(), nodeID},
		Payload:     marshaledRequest,
		Tags:        map[string]string{"reference": updatedOrder.Reference},
	}
	return tendermint.PostToChain(chainTX, "Order2")

}

// OrderSecret1 -
func (s *Service) OrderSecret1(req *api.OrderSecretRequest) (string, error) {
	orderReference := req.OrderReference
	var orderPart2CID string
	if err := s.Store.Get("order", orderReference, &orderPart2CID); err != nil {
		return "", err
	}

	nodeID := s.NodeID()
	recipientList, err := common.BuildRecipientList(s.Ipfs, nodeID, s.MasterFiduciaryNodeID())
	if err != nil {
		return "", err
	}
	remoteIDDoc, err := common.RetrieveIDDocFromIPFS(s.Ipfs, s.MasterFiduciaryNodeID())
	if err != nil {
		return "", err
	}

	_, _, blsSK, sikeSK, err := common.RetrieveIdentitySecrets(s.Store, nodeID)
	if err != nil {
		return "", err
	}

	//Retrieve the order from IPFS
	order, err := common.RetrieveOrderFromIPFS(s.Ipfs, orderPart2CID, sikeSK, nodeID, remoteIDDoc.BLSPublicKey)
	if err != nil {
		return "", errors.Wrap(err, "Fail to retrieve Order from IPFS")
	}

	if err := s.Plugin.ValidateOrderSecretRequest(req, *order); err != nil {
		return "", err
	}

	//Create a piece of data that is destined for the beneficiary, passed via the Master Fiduciary

	beneficiaryEncryptedData, extension, err := s.Plugin.ProduceBeneficiaryEncryptedData(blsSK, order, req)
	if err != nil {
		return "", err
	}

	if req.BeneficiaryIDDocumentCID != "" {
		order.BeneficiaryCID = req.BeneficiaryIDDocumentCID
	}

	//Create a request Object in IPFS
	orderPart3CID, err := common.CreateAndStorePart3(s.Ipfs, s.Store, order, orderPart2CID, nodeID, beneficiaryEncryptedData, recipientList)
	if err != nil {
		return "", err
	}

	//Post the address of the updated doc to the custody node
	request := &api.FulfillOrderSecretRequest{
		SenderDocumentCID: nodeID,
		OrderPart3CID:     orderPart3CID,
		Extension:         extension,
	}

	marshaledRequest, _ := json.Marshal(request)

	//Write the requests to the chain
	chainTX := &api.BlockChainTX{
		Processor:   api.TXFulfillOrderSecretRequest,
		SenderID:    nodeID,
		RecipientID: []string{s.MasterFiduciaryNodeID(), nodeID},
		Payload:     marshaledRequest,
	}
	//curl --data-binary '{"jsonrpc":"2.0","id":"anything","method":"broadcast_tx_commit","params": {"tx": "YWFhcT1hYWFxCg=="}}' -H 'content-type:text/plain;' http://localhost:26657

	return tendermint.PostToChain(chainTX, "OrderSecret1")
}

// OrderSecret2 -
func (s *Service) OrderSecret2(req *api.FulfillOrderSecretResponse) (string, error) {
	nodeID := s.NodeID()
	_, _, _, sikeSK, err := common.RetrieveIdentitySecrets(s.Store, nodeID)
	if err != nil {
		return "", err
	}

	remoteIDDoc, err := common.RetrieveIDDocFromIPFS(s.Ipfs, s.MasterFiduciaryNodeID())
	if err != nil {
		return "", err
	}

	//Retrieve the response Order from IPFS
	orderPart4, err := common.RetrieveOrderFromIPFS(s.Ipfs, req.OrderPart4CID, sikeSK, nodeID, remoteIDDoc.BLSPublicKey)
	if err != nil {
		return "", err
	}

	var beneficiariesSikeSK []byte
	var beneficiaryCID string

	beneficiaryCID = orderPart4.BeneficiaryCID

	_, beneficiariesSeed, _, beneficiariesSikeSK, err := common.RetrieveIdentitySecrets(s.Store, beneficiaryCID)
	if err != nil {
		return "", err
	}

	finalPrivateKey, finalPublicKey, ext, err := s.Plugin.ProduceFinalSecret(beneficiariesSeed, beneficiariesSikeSK, orderPart4, orderPart4, beneficiaryCID)
	if err != nil {
		return "", err
	}

	request := &api.OrderSecretResponse{
		Secret:         finalPrivateKey,
		Commitment:     finalPublicKey,
		OrderReference: orderPart4.Reference,
		Extension:      ext,
	}

	marshaledRequest, _ := json.Marshal(request)

	//Write the requests to the chain
	chainTX := &api.BlockChainTX{
		Processor:   api.TXOrderSecretResponse,
		SenderID:    nodeID,
		RecipientID: []string{s.MasterFiduciaryNodeID(), nodeID},
		Payload:     marshaledRequest,
	}
	//curl --data-binary '{"jsonrpc":"2.0","id":"anything","method":"broadcast_tx_commit","params": {"tx": "YWFhcT1hYWFxCg=="}}' -H 'content-type:text/plain;' http://localhost:26657

	return tendermint.PostToChain(chainTX, "OrderSecret2")

}
