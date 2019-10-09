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
Package service - defines core Milagro D-TA interface
*/
package service

import (
	"github.com/apache/incubator-milagro-dta/libs/transport"
	"github.com/apache/incubator-milagro-dta/pkg/api"
)

// Service is the CustodyService interface
type Service interface {
	//Order
	GetOrder(req *api.GetOrderRequest) (*api.GetOrderResponse, error)
	OrderList(req *api.OrderListRequest) (*api.OrderListResponse, error)

	//Order processing - REST access to create an Order & Redeem
	Order(req *api.OrderRequest) (string, error)
	OrderSecret(req *api.OrderSecretRequest) (string, error)

	//Fullfill processing
	FulfillOrder(tx *api.BlockChainTX) (string, error)

	NodeID() string
	MasterFiduciaryNodeID() string
	SetNodeID(nodeID string)
	SetMasterFiduciaryNodeID(masterFiduciaryNodeID string)

	//System
	Status(apiVersion, nopdeType string) (*api.StatusResponse, error)

	//Blockchain transactions
	Dump(tx *api.BlockChainTX) error //Decrypt and dump the order for debugging purposes.
	OrderSecret2(tx *api.BlockChainTX) (string, error)
	FulfillOrderSecret(tx *api.BlockChainTX) (string, error)
	Order2(tx *api.BlockChainTX) (string, error)
}

// Endpoints interface to register plugin specific endpoints
type Endpoints interface {
	Endpoints() (namespace string, endpoints transport.HTTPEndpoints)
}
