/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import "math"

// calculateReplicas는 totalReplicas와 canary weightPercent(0~100)를 받아
// stable과 canary 각각의 replica 수를 반환한다.
//
// 예) totalReplicas=10, weightPercent=30 → stable=7, canary=3
// 예) totalReplicas=10, weightPercent=100 → stable=0, canary=10
func calculateReplicas(totalReplicas, weightPercent int32) (stable, canary int32) {
	canary = int32(math.Round(float64(totalReplicas) * float64(weightPercent) / 100))
	stable = totalReplicas - canary
	return
}
