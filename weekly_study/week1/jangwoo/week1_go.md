# Week 1 - Go 언어 기초

---

## 사전 지식

### 1. Go란 무엇인가

> 공식 문서: "Go is an open source programming language that makes it easy to build simple, reliable, and efficient software."

Go(Golang)는 2009년 Google이 공개한 **정적 타입, 컴파일 언어**다.

**주요 특징**

- **정적 타입 + 컴파일**: 타입 오류를 컴파일 시점에 검출, 단일 바이너리로 배포
- **가비지 컬렉션**: 메모리 관리 자동화
- **내장 동시성**: 고루틴(Goroutine) + 채널(Channel)
- **간결한 문법**: 클래스 없음, 상속 없음, 예외(exception) 없음

```bash
go run main.go      # 실행
go build -o myapp   # 바이너리 빌드
go mod init <name>  # 모듈 초기화
go fmt ./...        # 코드 포맷팅
```

---

## 1. 기본 문법

### 1-1. 패키지와 임포트

모든 Go 파일은 **패키지 선언**으로 시작하고, 사용하지 않는 임포트는 **컴파일 오류**가 난다.

```go
package main

import (
    "fmt"
    "math"
)

func main() {
    fmt.Println("Hello, World!")
    fmt.Println(math.Sqrt(16)) // 4
}
```

---

### 1-2. 변수와 상수

```go
// var 키워드 (함수 밖에서도 사용 가능)
var name string = "Go"

// 단축 선언 := (함수 안에서만 사용) ← 가장 많이 사용
version := 1.21

// 상수
const Pi = 3.14159

// iota: 열거형 상수 자동 증가 ⭐ Go만의 특성
type Weekday int
const (
    Sunday Weekday = iota // 0
    Monday                // 1
    Tuesday               // 2
)
```

- 선언한 변수를 사용하지 않으면 **컴파일 오류**
- 초기화하지 않으면 **Zero Value** 자동 할당 (`int` → `0`, `string` → `""`, 포인터/슬라이스/맵 → `nil`)

---

### 1-3. 기본 타입

| 분류 | 타입 | 설명 |
|------|------|------|
| 정수 | `int`, `int32`, `int64` | 부호 있는 정수 |
| 실수 | `float32`, `float64` | 부동소수점 |
| 불리언 | `bool` | `true` / `false` |
| 문자열 | `string` | UTF-8 불변 문자열 |
| 바이트/룬 | `byte`, `rune` | `uint8`, `int32`의 별칭 |

> Go는 **암묵적 타입 변환이 없다**. 반드시 명시적 변환 필요: `float64(x)`

---

## 2. 함수

### 2-1. 기본 함수

```go
func add(x, y int) int {
    return x + y
}
```

### 2-2. 다중 반환값 ⭐ Go만의 특성

> 공식 문서: "A function can return any number of results."

Go는 함수가 **여러 값을 동시에 반환**할 수 있다. 이것이 Go **에러 처리 패턴**의 근간이다.

```go
func divide(a, b float64) (float64, error) {
    if b == 0 {
        return 0, fmt.Errorf("0으로 나눌 수 없습니다")
    }
    return a / b, nil
}

func main() {
    result, err := divide(10, 3)
    if err != nil {
        fmt.Println("에러:", err)
        return
    }
    fmt.Printf("결과: %.2f\n", result) // 결과: 3.33

    result2, _ := divide(20, 4) // 불필요한 반환값은 _ 로 무시
    fmt.Println(result2)        // 5
}
```

---

## 3. 데이터 구조

### 3-1. 슬라이스 (Slice)

> 공식 문서: "Slices are like references to arrays."

배열을 가리키는 **동적 크기의 참조 타입**이다.

```go
s := []int{1, 2, 3, 4, 5}
s = append(s, 6)           // 요소 추가

sub := s[1:4]              // [2 3 4] → 원본 배열을 참조 (복사 아님!)
sub[0] = 99
fmt.Println(s)             // [1 99 3 4 5 6] ← 원본도 변경됨

for i, v := range s {
    fmt.Printf("index=%d, value=%d\n", i, v)
}
```

### 3-2. 구조체 (Struct)

Go에는 클래스가 없다. **구조체에 메서드를 붙이는 방식**으로 OOP를 구현한다.

```go
type Person struct {
    Name string
    Age  int
}

// 값 수신자: 복사본 사용 (원본 수정 불가)
func (p Person) Greet() string {
    return fmt.Sprintf("안녕하세요, %s입니다!", p.Name)
}

// 포인터 수신자: 원본 구조체 수정 가능
func (p *Person) Birthday() {
    p.Age++
}

func main() {
    p := Person{Name: "Alice", Age: 30}
    fmt.Println(p.Greet()) // 안녕하세요, Alice입니다!
    p.Birthday()
    fmt.Println(p.Age) // 31
}
```

---

## 4. Go만의 핵심 개념

### 4-1. 에러 처리 ⭐

Go에는 `try/catch`가 없다. **에러는 반환값**으로 명시적으로 처리한다.

```
예외 방식 (Java/Python)         Go 방식
try {                           result, err := risky()
    result = risky()            if err != nil {
} catch (e) {                       handle(err)
    handle(e)                   }
}
→ 에러 흐름이 숨겨짐              → 에러 흐름이 명시적
```

```go
// 커스텀 에러 타입
type ValidationError struct {
    Field   string
    Message string
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("유효성 오류 - %s: %s", e.Field, e.Message)
}

func validateAge(age int) error {
    if age < 0 {
        return &ValidationError{Field: "age", Message: "음수 불가"}
    }
    return nil
}
```

---

### 4-2. defer ⭐

현재 함수가 종료될 때 **반드시 실행할 코드를 예약**하는 키워드다. 리소스 정리에 주로 사용한다.

```go
func readFile(path string) error {
    f, err := os.Open(path)
    if err != nil {
        return err
    }
    defer f.Close() // 함수 종료 시 자동 실행 (return, panic 포함)

    // 파일 처리 로직...
    return nil
}
```

여러 defer는 **LIFO(Last In, First Out)** 순서로 실행된다.

```go
func main() {
    defer fmt.Println("defer 1")
    defer fmt.Println("defer 2")
    defer fmt.Println("defer 3")
    fmt.Println("끝")
}
// 출력: 끝 → defer 3 → defer 2 → defer 1
```

---

### 4-3. 고루틴 (Goroutine) ⭐

> 공식 문서: "A goroutine is a lightweight thread managed by the Go runtime."

`go` 키워드 하나로 **경량 스레드**를 생성한다. OS 스레드(~1MB)보다 훨씬 가볍고 (~2KB), 수만 개를 동시에 실행할 수 있다.

```go
func say(s string) {
    for i := 0; i < 3; i++ {
        time.Sleep(100 * time.Millisecond)
        fmt.Println(s)
    }
}

func main() {
    go say("world")  // 새 고루틴에서 비동기 실행
    say("hello")     // 메인 고루틴에서 동기 실행
}
```

**sync.WaitGroup으로 완료 대기**

```go
var wg sync.WaitGroup

for i := 0; i < 3; i++ {
    wg.Add(1)
    go func(id int) {
        defer wg.Done()
        fmt.Printf("고루틴 %d 실행\n", id)
    }(i)
}

wg.Wait() // 모든 고루틴 완료까지 대기
```

---

### 4-4. 채널 (Channel) ⭐

> 공식 문서: "Do not communicate by sharing memory; instead, share memory by communicating."

고루틴 간 **안전하게 데이터를 주고받는 파이프**다.

```go
func main() {
    ch := make(chan int)

    go func() {
        ch <- 42  // 채널에 값 전송 (수신 전까지 블로킹)
    }()

    value := <-ch  // 채널에서 값 수신 (값이 올 때까지 블로킹)
    fmt.Println(value) // 42
}
```

**버퍼 채널**

```go
ch := make(chan int, 3) // 버퍼 크기 3 → 버퍼가 찰 때까지 블로킹 없음

ch <- 1
ch <- 2
ch <- 3

fmt.Println(<-ch) // 1
fmt.Println(<-ch) // 2
```

---

### 4-5. 인터페이스 - 암묵적 구현 ⭐

Go의 인터페이스는 **`implements` 선언 없이 메서드만 맞으면 자동으로 구현**된다.

```go
type Animal interface {
    Sound() string
}

type Dog struct{}
type Cat struct{}

func (d Dog) Sound() string { return "멍멍" }
func (c Cat) Sound() string { return "야옹" }

// Dog, Cat 모두 Animal 인터페이스를 자동으로 구현
func makeSound(a Animal) {
    fmt.Println(a.Sound())
}

func main() {
    makeSound(Dog{}) // 멍멍
    makeSound(Cat{}) // 야옹
}
```

---

## 정리 요약

```
Go 언어 핵심

다중 반환값
  func f() (결과, error) → 에러를 반환값으로 명시적 처리

에러 처리
  try/catch 없음 → if err != nil 패턴으로 처리

defer
  함수 종료 시 실행 예약 → 리소스 정리 (LIFO 순서)

고루틴 + 채널
  go func()    → 경량 스레드 (~2KB) 비동기 실행
  ch <- value  → 고루틴 간 안전한 통신

인터페이스
  implements 없이 메서드 시그니처만 맞으면 자동 구현

접근 제어
  대문자 시작 → 외부 공개 (exported)
  소문자 시작 → 내부 전용 (unexported)
```

---

## 참고 공식 문서

- [A Tour of Go](https://go.dev/tour/)
- [Effective Go](https://go.dev/doc/effective_go)
- [Go Blog - Error Handling](https://go.dev/blog/error-handling-and-go)
