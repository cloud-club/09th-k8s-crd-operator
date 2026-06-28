# Go 언어 기본 문법

> Kubernetes Operator 개발에 필요한 Go 언어의 핵심 문법을 정리합니다.

## 목차

1. [Go 언어의 특징](#1-go-언어의-특징)
2. [패키지와 모듈](#2-패키지와-모듈)
3. [변수, 상수, 타입](#3-변수-상수-타입)
4. [컬렉션 — 슬라이스와 맵](#4-컬렉션--슬라이스와-맵)
5. [제어 흐름](#5-제어-흐름)
6. [함수](#6-함수)
7. [구조체와 메서드](#7-구조체와-메서드)
8. [인터페이스](#8-인터페이스)
9. [포인터](#9-포인터)
10. [에러 처리](#10-에러-처리)
11. [고루틴과 채널](#11-고루틴과-채널)
12. [컨텍스트(Context)](#12-컨텍스트context)

## 1. Go 언어의 특징

Go(Golang)는 2009년 Google이 공개한 컴파일 언어입니다. Kubernetes 생태계에서 사실상 표준 언어로 자리 잡았는데, 몇 가지 설계 철학이 이 분야에 잘 맞기 때문입니다.

**빠른 컴파일**: 대규모 코드베이스도 수 초 안에 컴파일됩니다. 개발 사이클이 짧아집니다.

**정적 타입 + 간결한 문법**: Java처럼 타입 안전성을 보장하면서도 문법은 Python에 가까울 만큼 간결합니다.

**고루틴(goroutine)**: OS 스레드보다 훨씬 가벼운 동시성 단위를 언어 차원에서 지원합니다. Kubernetes의 컨트롤러 루프, 워커 큐 같은 패턴을 자연스럽게 구현할 수 있습니다.

**단일 바이너리 배포**: 외부 런타임 없이 바이너리 하나로 배포됩니다. 컨테이너 이미지를 최소화하기 좋습니다.

**명시적 에러 처리**: 예외(exception) 대신 에러 값을 반환하는 방식을 사용합니다. 어디서 무엇이 실패했는지 코드에서 명확히 드러납니다.

```bash
# Go 파일 실행 (컴파일 없이 바로 실행, 개발 시 편리)
go run main.go

# 빌드 후 실행
go build -o myapp main.go
./myapp
```

## 2. 패키지와 모듈

### 패키지

Go의 모든 파일은 패키지에 속합니다. 패키지는 관련 코드를 묶는 단위로, 디렉토리 하나가 패키지 하나에 대응합니다.

- `package main`: 실행 가능한 프로그램. `main()` 함수가 진입점
- 그 외 이름: 라이브러리 패키지. 다른 패키지에서 `import`해서 사용

```go
// cmd/main.go
package main

import (
    "fmt"
    "os"

    // 외부 패키지 — 모듈 경로로 지정
    "k8s.io/client-go/kubernetes"

    // 별칭(alias) — 이름이 충돌하거나 길 때 사용
    ctrl "sigs.k8s.io/controller-runtime"

    // 사이드이펙트 임포트 — init() 함수 실행이 목적
    // (클라우드 인증 플러그인 로딩 등)
    _ "k8s.io/client-go/plugin/pkg/client/auth"
)

func main() {
    // 패키지명.함수명 으로 사용
    fmt.Println("Hello, Kubernetes!")

    // 사용하지 않는 임포트는 컴파일 에러 — Go의 강제 규칙
    _ = os.Args       // 사용하지 않으면 이렇게 처리
    _ = kubernetes.NewForConfig
    _ = ctrl.Log
}
```

> **중요**: Go는 임포트했는데 사용하지 않으면 **컴파일 에러**가 납니다. "필요한 것만 쓴다"는 철학이 언어 수준에서 강제됩니다.

### 모듈과 패키지 경로

```
my-operator/                     ← 모듈 루트 (go.mod 위치)
├── go.mod                       ← module github.com/myorg/my-operator
├── cmd/
│   └── main.go                  ← package main
├── api/
│   └── v1/
│       └── types.go             ← package v1
└── internal/
    └── controller/
        └── reconciler.go        ← package controller
```

```go
// internal/controller/reconciler.go 에서 api/v1 패키지 임포트
import (
    examplev1 "github.com/myorg/my-operator/api/v1"
)
```

## 3. 변수, 상수, 타입

### 변수 선언

Go에는 변수를 선언하는 방법이 두 가지 있습니다.

```go
package main

import "fmt"

func main() {
    // 방법 1: var 키워드 — 타입을 명시하거나 초기값으로 추론
    var name string = "operator"
    var count int         // 초기값 없으면 제로값(zero value): int=0, string="", bool=false
    var enabled bool      // false

    // 방법 2: 단축 선언(:=) — 함수 내부에서만 사용 가능, 타입 자동 추론
    version := "v1.0.0"  // string으로 추론
    replicas := int32(3)  // 명시적 타입 변환

    // 여러 변수를 한 번에 선언할 때는 var 블록이 가독성 좋음
    var (
        namespace  = "default"
        maxRetries = 5
    )

    fmt.Println(name, count, enabled, version, replicas, namespace, maxRetries)
}
```

**제로값(Zero Value)**: Go는 변수를 선언하면 항상 타입에 맞는 기본값으로 초기화합니다. 초기화하지 않은 변수가 쓰레기값을 가지는 C와 다릅니다.

| 타입                             | 제로값           |
| -------------------------------- | ---------------- |
| `int`, `float64` 등 숫자         | `0`              |
| `string`                         | `""` (빈 문자열) |
| `bool`                           | `false`          |
| 포인터, 슬라이스, 맵, 함수, 채널 | `nil`            |

### 기본 타입

```go
var (
    // 정수 — 크기 명시
    a int    = 42       // 플랫폼에 따라 32 또는 64비트
    b int32  = 100      // Kubernetes API의 replicas 등에 자주 쓰임
    c int64  = 1000000
    d uint32 = 50       // 부호 없는 정수

    // 실수
    e float32 = 3.14
    f float64 = 3.14159265

    // 불리언
    g bool = true

    // 문자열 — UTF-8, 불변(immutable)
    h string = "hello"

    // 바이트 슬라이스 — 문자열과 자주 변환됨
    i []byte = []byte("hello")
    j string = string(i)
)
```

### 상수

```go
// 상수는 컴파일 타임에 결정됨 — 변수와 달리 주소를 가질 수 없음
const (
    MaxRetry    = 5
    APIVersion  = "example.com/v1"
    Pi          = 3.14159
)

// iota: 열거형 패턴 (0부터 자동 증가)
type Phase int

const (
    PhasePending Phase = iota  // 0
    PhaseRunning               // 1
    PhaseSucceeded             // 2
    PhaseFailed                // 3
)
```

### 타입 변환

Go는 **암묵적 타입 변환이 없습니다.** 항상 명시적으로 변환해야 합니다.

```go
var x int = 10
var y float64 = float64(x)  // int → float64
var z int32 = int32(x)      // int → int32

s := fmt.Sprintf("%d", x)   // int → string (숫자를 문자열로)
n, _ := strconv.Atoi("42")  // string → int (문자열을 숫자로)
```

## 4. 컬렉션 — 슬라이스와 맵

### 슬라이스

슬라이스는 Go에서 가장 자주 쓰는 컬렉션입니다. 배열과 달리 길이가 동적으로 늘어납니다.

```go
// 슬라이스 선언
var s1 []string              // nil 슬라이스 (len=0, cap=0)
s2 := []string{}             // 빈 슬라이스 (len=0)
s3 := []string{"a", "b", "c"}

// make로 초기 길이와 용량 지정
// make([]T, length, capacity)
// capacity를 알면 미리 지정해 append 시 재할당 횟수를 줄임
nums := make([]int, 0, 10)

// append: 새 요소 추가 (반환값을 반드시 받아야 함)
s3 = append(s3, "d")
s3 = append(s3, "e", "f")     // 여러 개 한번에 추가
s3 = append(s3, s2...)         // 다른 슬라이스 이어 붙이기 (...로 펼침)

// 인덱싱과 슬라이싱
fmt.Println(s3[0])            // 첫 번째 요소
sub := s3[1:3]                // 인덱스 1 이상 3 미만 → ["b", "c"]
tail := s3[2:]                // 인덱스 2부터 끝까지
head := s3[:2]                // 처음부터 인덱스 2 미만

// 길이와 용량
fmt.Println(len(s3), cap(s3))

// 순회
for i, v := range s3 {
    fmt.Printf("[%d] %s\n", i, v)
}

// 인덱스만 필요할 때
for i := range s3 {
    fmt.Println(i)
}

// 값만 필요할 때 (_로 인덱스 무시)
for _, v := range s3 {
    fmt.Println(v)
}
```

> **주의**: 슬라이스는 내부적으로 배열을 가리키는 포인터입니다. `sub := s[1:3]`처럼 슬라이싱하면 원본 배열을 공유합니다. 독립적인 복사본이 필요하면 `copy(dst, src)`를 사용하세요.

### 맵

맵은 키-값 쌍을 저장합니다. Kubernetes의 레이블, 어노테이션이 `map[string]string` 타입입니다.

```go
// 맵 선언 및 초기화
labels := map[string]string{
    "app":     "my-operator",
    "version": "v1",
    "env":     "production",
}

// make로 생성 (초기 용량 힌트 제공 가능)
annotations := make(map[string]string)

// 값 추가 및 수정
labels["tier"] = "backend"

// 값 읽기
app := labels["app"]  // 없는 키면 타입의 제로값("") 반환

// 키 존재 여부 확인 — 두 번째 반환값(ok)으로 판단
// 없는 키를 읽으면 제로값이 반환되므로 존재 여부는 반드시 ok로 확인해야 함
if val, ok := labels["tier"]; ok {
    fmt.Println("tier:", val)
} else {
    fmt.Println("tier 키 없음")
}

// 키 삭제
delete(labels, "env")

// 순회 (순서 보장 안 됨 — 정렬이 필요하면 키를 슬라이스로 추출 후 sort)
for k, v := range labels {
    fmt.Printf("%s=%s\n", k, v)
}

// 길이
fmt.Println(len(labels))
```

> **주의**: nil 맵에 쓰기를 시도하면 패닉이 납니다. `var m map[string]string` 선언 후 바로 값을 할당하면 에러입니다. 반드시 `make` 또는 리터럴로 초기화하세요.

## 5. 제어 흐름

### if / else

```go
// 기본 if
if x > 10 {
    fmt.Println("크다")
} else if x == 10 {
    fmt.Println("같다")
} else {
    fmt.Println("작다")
}

// 초기화 구문 포함 — 변수 스코프가 if 블록 안으로 제한됨
// 에러 처리에 자주 쓰는 패턴
if err := doSomething(); err != nil {
    fmt.Println("에러:", err)
    return
}
```

### for — Go의 유일한 반복문

Go에는 `while`이 없습니다. `for`가 모든 반복을 담당합니다.

```go
// C 스타일 for
for i := 0; i < 5; i++ {
    fmt.Println(i)
}

// while처럼 사용
count := 0
for count < 5 {
    count++
}

// 무한 루프 (break로 탈출)
for {
    if shouldStop() { break }
}

// range — 슬라이스, 맵, 채널, 문자열 순회
for i, v := range []int{1, 2, 3} { fmt.Println(i, v) }
for k, v := range map[string]int{"a": 1} { fmt.Println(k, v) }
```

### switch

```go
phase := "Running"

switch phase {
case "Pending":
    fmt.Println("대기 중")
case "Running", "Active":  // 여러 값을 하나의 case로
    fmt.Println("실행 중")
case "Failed":
    fmt.Println("실패")
default:
    fmt.Println("알 수 없는 상태")
}

// 타입 스위치 — 인터페이스 타입을 구체 타입으로 분기할 때 매우 유용
func describe(i interface{}) {
    switch v := i.(type) {
    case int:
        fmt.Printf("정수: %d\n", v)
    case string:
        fmt.Printf("문자열: %s\n", v)
    case bool:
        fmt.Printf("불리언: %v\n", v)
    default:
        fmt.Printf("알 수 없는 타입: %T\n", v)
    }
}
```

## 6. 함수

### 기본 함수

```go
// 기본 형태: func 이름(매개변수) 반환타입
func add(a, b int) int {
    return a + b
}

// 매개변수 타입이 같으면 마지막에만 타입 명시 가능
func multiply(a, b int) int {
    return a * b
}
```

### 다중 반환값 — Go의 핵심 패턴

Go 함수는 값을 여러 개 반환할 수 있습니다. 에러를 마지막 반환값으로 돌려주는 것이 관례입니다.

```go
// (결과값, 에러) 패턴 — Go 전반에서 쓰이는 관례
func divide(a, b float64) (float64, error) {
    if b == 0 {
        return 0, fmt.Errorf("0으로 나눌 수 없습니다")
    }
    return a / b, nil
}

// 호출 측에서 에러를 반드시 처리해야 함 (무시하려면 _ 사용)
result, err := divide(10, 3)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("%.4f\n", result)

// 이름 있는 반환값 — 함수 내에서 변수처럼 사용, naked return 가능
func minMax(nums []int) (min, max int) {
    min, max = nums[0], nums[0]
    for _, n := range nums[1:] {
        if n < min { min = n }
        if n > max { max = n }
    }
    return  // naked return: 이름 있는 반환값을 그대로 반환
}
```

### defer — 함수 종료 시 실행 보장

`defer`는 해당 함수가 끝날 때 실행될 코드를 등록합니다. 리소스 정리(파일 닫기, 잠금 해제 등)에 자주 씁니다. 여러 `defer`가 있으면 **LIFO(후입선출)** 순서로 실행됩니다.

```go
func readFile(path string) error {
    f, err := os.Open(path)
    if err != nil {
        return fmt.Errorf("파일 열기 실패: %w", err)
    }
    defer f.Close()  // 함수 종료 시 자동으로 닫힘 (에러 반환 시에도)

    // 여러 defer — 역순으로 실행
    defer fmt.Println("3")  // 마지막에 실행
    defer fmt.Println("2")
    defer fmt.Println("1")  // 먼저 실행

    // 파일 처리 로직...
    return nil
}
```

### 일급 함수와 클로저

Go에서 함수는 값입니다. 변수에 담거나, 인수로 넘기거나, 반환할 수 있습니다.

```go
// 함수를 변수에 담기
double := func(x int) int { return x * 2 }
fmt.Println(double(5))  // 10

// 함수를 인수로 받기 — 고차 함수
func apply(nums []int, f func(int) int) []int {
    result := make([]int, len(nums))
    for i, n := range nums {
        result[i] = f(n)
    }
    return result
}

squares := apply([]int{1, 2, 3, 4}, func(x int) int { return x * x })
// [1, 4, 9, 16]

// 클로저: 외부 변수를 캡처하는 함수
// count 변수가 반환된 함수와 함께 살아있음
func counter() func() int {
    count := 0
    return func() int {
        count++  // 외부 변수 count를 캡처
        return count
    }
}

c := counter()
fmt.Println(c(), c(), c())  // 1 2 3
```

### 가변 인수

```go
// ...타입으로 가변 인수 선언
func sum(nums ...int) int {
    total := 0
    for _, n := range nums {
        total += n
    }
    return total
}

fmt.Println(sum(1, 2, 3))        // 6
fmt.Println(sum(1, 2, 3, 4, 5)) // 15

// 슬라이스를 가변 인수로 펼쳐서 전달
nums := []int{1, 2, 3}
fmt.Println(sum(nums...))  // ...으로 펼침
```

## 7. 구조체와 메서드

### 구조체

Go에는 클래스가 없습니다. 구조체(struct)가 데이터를 묶는 역할을 합니다. Kubernetes의 CR(Custom Resource) 타입도 Go 구조체로 정의됩니다.

```go
// 구조체 정의
type AppConfig struct {
    Name      string
    Namespace string
    Replicas  int32
    Labels    map[string]string
    // 소문자로 시작하면 외부 패키지에서 접근 불가 (unexported)
    internalID string
}

// 구조체 초기화 방법
// 방법 1: 필드명 명시 (권장 — 필드 순서 바뀌어도 안전)
cfg := AppConfig{
    Name:      "my-app",
    Namespace: "default",
    Replicas:  3,
    Labels:    map[string]string{"app": "my-app"},
}

// 방법 2: 위치 기반 (모든 필드를 순서대로 지정해야 함 — 잘 안 씀)
cfg2 := AppConfig{"my-app", "default", 3, nil, ""}

// 필드 접근
fmt.Println(cfg.Name, cfg.Replicas)
cfg.Replicas = 5

// 구조체 포인터
p := &cfg                   // 포인터 생성
fmt.Println(p.Name)         // (*p).Name 와 동일 — Go가 자동으로 역참조
p.Replicas = 10             // 포인터를 통한 수정

// new() — 제로값으로 초기화된 구조체 포인터 반환
cfg3 := new(AppConfig)      // &AppConfig{}와 동일
cfg3.Name = "new-app"
```

### 구조체 임베딩

Go에는 상속이 없지만 임베딩으로 비슷한 효과를 냅니다.

```go
type BaseResource struct {
    APIVersion string
    Kind       string
}

func (b BaseResource) TypeMeta() string {
    return b.APIVersion + "/" + b.Kind
}

type MyApp struct {
    BaseResource         // 임베딩 — BaseResource의 필드와 메서드를 그대로 사용 가능
    Spec   MyAppSpec
    Status MyAppStatus
}

app := MyApp{
    BaseResource: BaseResource{
        APIVersion: "example.com/v1",
        Kind:       "MyApp",
    },
}

// 임베딩된 필드와 메서드에 직접 접근
fmt.Println(app.APIVersion)  // app.BaseResource.APIVersion와 동일
fmt.Println(app.TypeMeta())  // BaseResource의 메서드 직접 호출
```

### 메서드

메서드는 특정 타입에 연결된 함수입니다. 리시버(receiver)라는 개념으로 타입과 연결합니다.

```go
type AppConfig struct {
    Name     string
    Replicas int32
}

// 값 리시버: AppConfig의 복사본에 작동 — 원본을 수정하지 않음
// 읽기 전용 작업이나 구조체가 작을 때 사용
func (c AppConfig) String() string {
    return fmt.Sprintf("%s (replicas=%d)", c.Name, c.Replicas)
}

// 포인터 리시버: 원본 AppConfig를 직접 수정
// 구조체를 수정하거나 크기가 클 때 사용 (복사 비용 절약)
func (c *AppConfig) Scale(n int32) {
    c.Replicas = n  // 원본 수정
}

func (c *AppConfig) Validate() error {
    if c.Name == "" {
        return fmt.Errorf("name은 비어있을 수 없습니다")
    }
    if c.Replicas < 0 || c.Replicas > 100 {
        return fmt.Errorf("replicas는 0~100 사이여야 합니다: %d", c.Replicas)
    }
    return nil
}

// 사용
cfg := &AppConfig{Name: "my-app", Replicas: 3}
fmt.Println(cfg)         // String() 자동 호출 (fmt.Stringer 인터페이스)
cfg.Scale(5)
if err := cfg.Validate(); err != nil {
    log.Fatal(err)
}
```

> **관례**: 한 타입의 메서드에서 값 리시버와 포인터 리시버를 섞어 쓰지 않는 것이 좋습니다. 수정이 필요한 메서드가 하나라도 있으면 모든 메서드를 포인터 리시버로 통일합니다.

## 8. 인터페이스

인터페이스는 메서드 시그니처의 집합입니다. Go의 인터페이스는 **암묵적(implicit) 구현**이 특징입니다. 구현한다고 명시적으로 선언하지 않아도, 인터페이스의 메서드를 모두 구현하면 자동으로 인터페이스를 만족합니다.

```go
// 인터페이스 정의
type Reconciler interface {
    Reconcile(ctx context.Context, name, namespace string) error
    GetName() string
}

// 인터페이스 구현 — "implements Reconciler"라는 선언 없음
// 메서드를 모두 구현하면 자동으로 Reconciler를 만족
type DeploymentReconciler struct {
    Name string
}

func (r *DeploymentReconciler) Reconcile(ctx context.Context, name, namespace string) error {
    fmt.Printf("[%s] Reconciling deployment %s/%s\n", r.Name, namespace, name)
    return nil
}

func (r *DeploymentReconciler) GetName() string {
    return r.Name
}

// 인터페이스 타입으로 인수 받기 — 구체 타입에 의존하지 않음
// DeploymentReconciler 외에 다른 Reconciler 구현체도 받을 수 있음
func RunReconcile(rec Reconciler, name, ns string) {
    fmt.Printf("Using reconciler: %s\n", rec.GetName())
    if err := rec.Reconcile(context.Background(), name, ns); err != nil {
        fmt.Println("에러:", err)
    }
}

func main() {
    rec := &DeploymentReconciler{Name: "deployment-reconciler"}
    RunReconcile(rec, "my-app", "default")
}
```

### 표준 라이브러리의 핵심 인터페이스

```go
// fmt.Stringer — String() 구현 시 fmt.Println 등에서 자동 사용
type Stringer interface {
    String() string
}

// error — Go의 에러 인터페이스
type error interface {
    Error() string
}

// io.Reader / io.Writer — I/O 추상화
type Reader interface {
    Read(p []byte) (n int, err error)
}
```

### 타입 단언과 타입 스위치

인터페이스 타입에서 구체 타입으로 꺼내는 방법입니다.

```go
var rec Reconciler = &DeploymentReconciler{Name: "dr"}

// 타입 단언 — 잘못된 타입이면 패닉
dr := rec.(*DeploymentReconciler)
fmt.Println(dr.Name)

// 안전한 타입 단언 — ok로 확인
if dr, ok := rec.(*DeploymentReconciler); ok {
    fmt.Println("DeploymentReconciler:", dr.Name)
}

// 빈 인터페이스 (any) — 모든 타입을 담을 수 있음
// Kubernetes의 runtime.Object, JSON unmarshaling 등에서 등장
var anything any = "hello"
anything = 42
anything = struct{ X int }{X: 1}
```

## 9. 포인터

### 포인터란?

포인터는 **다른 변수의 메모리 주소**를 저장하는 변수입니다. Go는 C처럼 포인터를 가지지만, 포인터 연산(주소 증감 등)은 지원하지 않아 더 안전합니다.

```go
x := 42
p := &x          // &: 주소 연산자. p는 x의 주소를 가리킴
fmt.Println(p)   // 주소값 출력: 0xc000012090 같은 형태
fmt.Println(*p)  // *: 역참조(dereference). p가 가리키는 값 = 42

*p = 100         // p를 통해 x의 값을 수정
fmt.Println(x)   // 100
```

### 포인터가 필요한 이유

Go는 함수에 값을 전달할 때 기본적으로 **복사**합니다. 구조체를 수정하거나 큰 데이터를 효율적으로 전달하려면 포인터를 사용합니다.

```go
type Config struct{ Replicas int32 }

// 값으로 전달 — 복사본을 수정하므로 원본 변경 없음
func scaleByValue(c Config, n int32) {
    c.Replicas = n  // 복사본만 수정됨
}

// 포인터로 전달 — 원본을 직접 수정
func scaleByPointer(c *Config, n int32) {
    c.Replicas = n  // (*c).Replicas = n 와 동일
}

cfg := Config{Replicas: 3}
scaleByValue(cfg, 10)
fmt.Println(cfg.Replicas)  // 3 (변경 안 됨)

scaleByPointer(&cfg, 10)
fmt.Println(cfg.Replicas)  // 10 (변경됨)
```

### nil 포인터 주의

포인터의 제로값은 `nil`입니다. `nil` 포인터를 역참조하면 패닉이 발생합니다.

```go
var p *Config  // nil

// p.Replicas  // 패닉! nil pointer dereference

// nil 체크를 먼저
if p != nil {
    fmt.Println(p.Replicas)
}

// 함수에서 포인터 반환 — 힙에 할당되어 함수 종료 후에도 살아있음
func newConfig(name string) *Config {
    return &Config{Replicas: 3}  // 주소를 반환해도 안전
}
```

## 10. 에러 처리

### Go의 에러 철학

Go에는 try/catch가 없습니다. 에러는 반환값으로 처리합니다. "에러는 숨기지 말고 명시적으로 처리하라"는 철학입니다.

```go
// error 인터페이스를 구현하는 커스텀 에러 타입
type NotFoundError struct {
    Resource  string
    Namespace string
    Name      string
}

func (e *NotFoundError) Error() string {
    return fmt.Sprintf("%s %q not found in namespace %q",
        e.Resource, e.Name, e.Namespace)
}

// Sentinel 에러 — 특정 에러를 식별하기 위한 변수
var (
    ErrAlreadyExists = errors.New("resource already exists")
    ErrInvalidSpec   = errors.New("invalid spec")
)
```

### 에러 래핑과 언래핑

에러를 체인으로 엮어 원인을 추적할 수 있습니다. `%w` 동사로 에러를 래핑하고, `errors.Is` / `errors.As`로 풀어냅니다.

```go
func getResource(name, ns string) error {
    // 내부 에러를 컨텍스트와 함께 래핑
    err := &NotFoundError{Resource: "MyApp", Name: name, Namespace: ns}
    return fmt.Errorf("getResource: %w", err)  // %w로 래핑
}

func reconcile(name, ns string) error {
    if err := getResource(name, ns); err != nil {
        // 다시 래핑하면서 컨텍스트 추가
        return fmt.Errorf("reconcile failed: %w", err)
    }
    return nil
}

func main() {
    err := reconcile("my-app", "default")
    if err == nil { return }

    // errors.Is: 래핑된 에러 체인에서 Sentinel 에러 찾기
    if errors.Is(err, ErrAlreadyExists) {
        fmt.Println("이미 존재함")
    }

    // errors.As: 래핑된 에러 체인에서 특정 타입 찾기
    var nfe *NotFoundError
    if errors.As(err, &nfe) {
        fmt.Printf("%s %q를 찾을 수 없음 (ns: %s)\n",
            nfe.Resource, nfe.Name, nfe.Namespace)
    }

    // 에러 체인 전체 메시지
    fmt.Println(err)
    // reconcile failed: getResource: MyApp "my-app" not found in namespace "default"
}
```

### panic과 recover

`panic`은 복구 불가능한 상황에서 프로그램을 중단시킵니다. `recover`는 패닉을 잡아 정상화합니다. 일반적인 에러 처리에는 사용하지 않습니다.

```go
func safeRun(f func()) (err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("패닉 복구: %v", r)
        }
    }()
    f()
    return nil
}

err := safeRun(func() {
    panic("예상치 못한 상황")
})
fmt.Println(err)  // 패닉 복구: 예상치 못한 상황
```

## 11. 고루틴과 채널

### 고루틴

고루틴은 Go 런타임이 관리하는 경량 스레드입니다. OS 스레드보다 훨씬 적은 메모리(초기 약 2KB)를 사용하고 수천~수만 개를 동시에 실행할 수 있습니다.

```go
// go 키워드로 함수를 비동기 실행
go func() {
    fmt.Println("고루틴에서 실행")
}()

// 익명 함수 대신 일반 함수도 가능
go doWork()
```

### sync.WaitGroup — 여러 고루틴 완료 대기

```go
func worker(id int, wg *sync.WaitGroup) {
    defer wg.Done()  // 작업 완료 시 카운터 감소
    fmt.Printf("Worker %d 시작\n", id)
    time.Sleep(100 * time.Millisecond)
    fmt.Printf("Worker %d 완료\n", id)
}

var wg sync.WaitGroup

for i := 1; i <= 5; i++ {
    wg.Add(1)         // 카운터 증가
    go worker(i, &wg) // 고루틴 실행
}

wg.Wait()  // 카운터가 0이 될 때까지 블록
fmt.Println("모든 워커 완료")
```

### 채널 — 고루틴 간 안전한 데이터 전달

채널은 고루틴 사이에서 데이터를 주고받는 파이프입니다. "통신으로 메모리를 공유하라"는 Go의 핵심 철학을 구현합니다.

```go
// 버퍼 없는 채널 — 송신자와 수신자가 동시에 준비되어야 함
ch := make(chan int)

// 버퍼 있는 채널 — 버퍼가 찰 때까지 블록 없이 송신 가능
buffered := make(chan int, 10)

// 단방향 채널 — 함수 시그니처에서 방향을 제한할 때
func produce(out chan<- int) { out <- 42 }  // 쓰기 전용
func consume(in <-chan int)  { fmt.Println(<-in) }  // 읽기 전용

// 채널 닫기 — 더 보낼 값이 없음을 알림
// 닫힌 채널에서 range는 모든 값을 읽은 후 종료
func producer(ch chan<- string) {
    items := []string{"a", "b", "c"}
    for _, item := range items {
        ch <- item
    }
    close(ch)  // 반드시 송신 측에서 닫음
}

ch2 := make(chan string, 3)
go producer(ch2)
for item := range ch2 {  // close 되면 자동 종료
    fmt.Println(item)
}
```

### select — 여러 채널 동시 대기

`select`는 `switch`와 비슷하지만 채널 연산에 작동합니다. 여러 채널 중 준비된 것을 먼저 처리합니다.

```go
func process(ctx context.Context, jobs <-chan string) {
    for {
        select {
        case job, ok := <-jobs:
            if !ok {
                fmt.Println("채널 닫힘")
                return
            }
            fmt.Println("처리:", job)

        case <-ctx.Done():
            // 컨텍스트 취소 시 종료 — Operator 종료 처리에 필수
            fmt.Println("컨텍스트 취소:", ctx.Err())
            return

        case <-time.After(5 * time.Second):
            fmt.Println("타임아웃")
            return
        }
    }
}
```

### sync.Mutex — 공유 데이터 보호

여러 고루틴이 같은 데이터에 접근할 때 뮤텍스로 동시 접근을 막습니다.

```go
type SafeCounter struct {
    mu    sync.Mutex
    count int
}

func (c *SafeCounter) Increment() {
    c.mu.Lock()
    defer c.mu.Unlock()  // defer로 항상 Unlock 보장
    c.count++
}

func (c *SafeCounter) Value() int {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.count
}

counter := &SafeCounter{}
var wg sync.WaitGroup

for i := 0; i < 1000; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        counter.Increment()
    }()
}

wg.Wait()
fmt.Println(counter.Value())  // 정확히 1000
```

## 12. 컨텍스트(Context)

### Context가 필요한 이유

Kubernetes Operator는 여러 고루틴이 동시에 API 서버와 통신합니다. 운영자가 Operator를 종료하거나 요청이 타임아웃되었을 때, 진행 중인 모든 작업을 깔끔하게 중단해야 합니다. `context.Context`는 이런 취소 신호와 메타데이터를 고루틴 트리 전체에 전파하는 메커니즘입니다.

controller-runtime의 `Reconcile` 함수도 첫 번째 인수로 `context.Context`를 받습니다.

```go
// Context 계층 구조
// Background → WithTimeout → WithCancel
//                            └─ 자식 고루틴들이 모두 이 ctx를 전달받음
```

### 주요 Context 생성 함수

```go
// context.Background(): 최상위 컨텍스트, 취소 없음 (main, 테스트에서 시작점)
ctx := context.Background()

// context.TODO(): 아직 어떤 컨텍스트를 써야 할지 모를 때 임시로 사용
ctx2 := context.TODO()

// WithCancel: 명시적으로 취소할 수 있는 컨텍스트
ctx3, cancel := context.WithCancel(context.Background())
defer cancel()  // 반드시 cancel 호출 (리소스 누수 방지)
go func() {
    time.Sleep(1 * time.Second)
    cancel()  // 1초 후 취소
}()

// WithTimeout: 지정 시간 후 자동 취소
ctx4, cancel4 := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel4()

// WithDeadline: 특정 시각에 자동 취소
deadline := time.Now().Add(1 * time.Minute)
ctx5, cancel5 := context.WithDeadline(context.Background(), deadline)
defer cancel5()

// WithValue: 키-값 쌍을 컨텍스트에 저장
// 주로 요청 ID, 인증 토큰 등 요청 범위 메타데이터에 사용
// (설정값 전달에는 남용하지 말 것)
type contextKey string  // 충돌 방지를 위해 전용 타입 사용
ctx6 := context.WithValue(context.Background(),
    contextKey("requestID"), "req-abc-123")

if id, ok := ctx6.Value(contextKey("requestID")).(string); ok {
    fmt.Println("Request ID:", id)
}
```

### Reconcile 함수에서의 Context 활용

```go
func (r *MyAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    logger := log.FromContext(ctx)  // ctx에서 로거 추출 (request ID 등 포함)

    // ctx를 API 호출에 전달 — 취소 신호가 여기까지 전파됨
    myApp := &examplev1.MyApp{}
    if err := r.Get(ctx, req.NamespacedName, myApp); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // 오래 걸리는 작업에 별도 타임아웃 설정
    opCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
    defer cancel()

    if err := r.callExternalAPI(opCtx, myApp); err != nil {
        // ctx.Err()로 취소 원인 확인
        if ctx.Err() != nil {
            logger.Info("컨텍스트 취소됨", "reason", ctx.Err())
            return ctrl.Result{}, nil
        }
        return ctrl.Result{}, err
    }

    return ctrl.Result{}, nil
}

// ctx.Done()으로 취소 감지
func (r *MyAppReconciler) callExternalAPI(ctx context.Context, app *examplev1.MyApp) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
        // 실제 API 호출
        return nil
    }
}
```

> **핵심 규칙**:
>
> 1. Context는 구조체에 저장하지 말고, 함수의 **첫 번째 인수**로 항상 전달
> 2. `cancel`은 반드시 `defer cancel()`로 호출 (리소스 누수 방지)
> 3. 자식 Context는 부모가 취소되면 함께 취소됨
