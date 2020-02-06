/*
 Licensed to the Apache Software Foundation (ASF) under one
 or more contributor license agreements.  See the NOTICE file
 distributed with this work for additional information
 regarding copyright ownership.  The ASF licenses this file
 to you under the Apache License, Version 2.0 (the
 "License"); you may not use this file except in compliance
 with the License.  You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/
package webservice

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/apache/incubator-yunikorn-core/pkg/cache"
	"github.com/apache/incubator-yunikorn-core/pkg/log"
)

var gClusterInfo *cache.ClusterInfo

type WebService struct {
	httpServer  *http.Server
	clusterInfo *cache.ClusterInfo
	lock        sync.RWMutex
}

func NewRouter() *mux.Router {
	router := mux.NewRouter().StrictSlash(true)
	for _, route := range routes {
		var handler http.Handler

		handler = route.HandlerFunc
		handler = Logger(handler, route.Name)

		router.
			Methods(route.Method).
			Path(route.Pattern).
			Name(route.Name).
			Handler(handler)
	}
	return router
}

func Logger(inner http.Handler, name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		inner.ServeHTTP(w, r)

		log.Logger().Debug(fmt.Sprintf("%s\t%s\t%s\t%s",
			r.Method, r.RequestURI, name, time.Since(start)))
	})
}

// TODO we need the port to be configurable
func (m *WebService) StartWebApp() {
	router := NewRouter()
	m.httpServer = &http.Server{Addr: ":9080", Handler: router}

	log.Logger().Info("web-app started", zap.Int("port", 9080))
	go func() {
		httpError := m.httpServer.ListenAndServe()
		if httpError != nil && httpError != http.ErrServerClosed {
			log.Logger().Error("HTTP serving error",
				zap.Error(httpError))
		}
	}()
}

func NewWebApp(clusterInfo *cache.ClusterInfo) *WebService {
	m := &WebService{}
	gClusterInfo = clusterInfo
	return m
}

func (m *WebService) StopWebApp() error {
	if m.httpServer != nil {
		// graceful shutdown in 5 seconds
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return m.httpServer.Shutdown(ctx)
	}

	return nil
}
