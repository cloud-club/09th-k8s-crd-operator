1주차 - Go 언어 기본 개념 학습

# 1. 프로젝트 구조

### 프로젝트의 기본 구조

Go언어의 프로젝트는 `모듈(프로젝트)` → `패키지` → `소스코드(.go)` 로 구성되어있다.

- **모듈** : `go.mod` 파일을 통해 정의되는 하나의 프로젝트이자 배포 단위이다.
- **패키지 :** 관련된 Go 파일들의 묶음을 의미한다.

 >   패키지는 주로 디렉토리 단위로 분류되지만, 항상 `디렉토리 이름` = `패키지 이름` 을 따르는 것은 아니다.     

---

# 2. 패키지 구조

패키지는 `.go` 파일들의 묶음이자 코드의 기본 단위이다.

**모든 코드는 무조건 패키지 안에 시작해야 하며, 모든 `.go` 파일은 `package` 로 시작 되어야 한다.**

패키지는 기본적으로 디렉토리 단위로 정의되지만, 디렉토리의 이름이 무조건 패키지 이름이 되는 것은 아니다.

### 2-1. Package main

Go에서 실행 가능한 파일이 되려면 **`package main`과 그 안에 `func main()` 함수가 동시에 있어야 한다.**

그리고 `package main` 으로 선언된 파일이 위치한 디렉토리가 `main` 패키지가 되고,

`main` 패키지 내부의 `main()` 함수가 프로젝트의 시작점이 되는 것이다.

```go
package main

import "fmt"

func main(){
	fmt.Print("Hello, World!")
}
```

반면, 아래와 같이 패키지가 main이 아니라면 직접 실행되지 않고, 다른 패키지에 의해 import 되어야 실행 된다.

```go
package math

func Add(a, b int) int{
	return a + b
}
```

---

# 3. 프로젝트 실행

go 언어 코드를 실행하는 방법은 2가지가 있다.

- `go build`
- `go run`

### 3-1. go build

go 언어로 작성한 코드를 빌드하고, 바이너리 파일(실행 파일)을 생성하여 이를 실행시킨다.

```bash
go build [-o output] [build flags] [packages]
```

1. 패키지 단위의 파일을 컴파일하여 오브젝트 파일을 생성한다.
2. `import` 경로를 읽고, 필요한 의존성을 포함한다.
3. 링킹 작업을 통해 여러 오브젝트 파일을 하나의 실행 파일로 결합한다.
4. 하나의 코드 실행 파일을 생성한다.
    
    (Windows의 경우, `.exe` 파일)
    

따라서 아래와 같이 실행 파일을 실행함으로써 코드를 실행시킬 수 있다.

```bash
# 빌드 (모듈 이름으로 실행 파일 생성)
go build

# 빌드 (다른 이름 또는 경로로 실행 파일 생성 시 -o 옵션 사용)
go build -o main

# 실행
./main
```

> 이에 따라 Go언어의 장점은 경량화가 가능하다는 것이다. 바이너리 파일을 생성함으로써 별도의 Go언어 설치 없이 프로그램 실행이 가능하고, > 이는 컨테이너 이미지로 실행했을 때 해당 프로그래밍 언어를 실행하기 위한 SDK가 필요 없어 용량이 줄어든다. 

### 3-2. go run

go 언어로 작성 코드의 임시 컴파일을 진행하고, 이를 실행시킨다.

```bash
go run main.go
```

1. 패키지 단위의 파일을 컴파일 하여 오브젝트 파일을 생성한다.
2. 임시 바이너리 파일을 생성한다.
3. 바이너리 파일을 실행 후, 삭제한다.

이 방식은 **개발용 또는 테스트용**으로 빠른 실행을 위해 많이 사용 된다.

# 4. 변수 선언

### 4-1. `var` , `:=`

기본적인 변수 선언은 아래와 같다.

```java
var x int = 10
```

변수의 타입을 예상할 수 있다면 변수의 타입은 생략이 가능하다.

```java
var x = 10
```

타입을 명시하여 변수를 초기화하지 않고 선언만 할 수 있다.

```java
var x int
```

여러 개의 변수를 한 줄에 선언할 수도 있다.

```java
var x, y int = 10, 20
```

다음과 같이 `var` 키워드를 이용하여 한번에 변수를 선언할 수도 있다.

```java
	var (
		x int = 10

		b byte = 100

		sum3 int  = x + int(b)
		sum4 byte = byte(x) + b
	)
	
```

`var` 키워드를 생략하고 변수의 값을 `:=` 연산자로 할당할 수 있다.

```java
var x = 10
x := 10
```

# 5. Type

`struct` 와 같이, **기본 타입** 혹은 **복합 타입**의 리터럴을 사용해서 사용자 정의 기반의 구체적인 값을 설정할 수 있다.

```go
type Score int
type Converter func(string)Score
type TeamScores map[string]Score
```

>**abstract type과 type의 차이**
>
>**abstract type의 경우** type이 어떤 행위를 해야 하고, 어떤 형태를 갖춰야 하는지를 정의한다.
>
>**type의 경우** 구체적으로 해당 타입에 어떤 데이터가 저장되고, 구현체를 정의한다.

# 6. Methods

Go 또한 사용자 정의 타입을 활용하여 **메소드**를 구현할 수 있도록 지원한다.

이 메소드는 Java에서 클래스의 함수를 정의하는 메소드와 유사할 수 있는데, **Go에서는 Type에 대한 함수를 정의**하는 것이라고 볼 수 있다.

```go
type person struct{
	FirstName string
	LastName  string
	Age       int
}

func (p Person) String() string{
		return fmt.Sprintf("%s %s, age %d", p.FirstName, p.LastName, p.Age)
}
```

### 6-1. Receiver

메소드의 선언 방식은 함수 선언과 유사하지만, **receiver**를 구체화 하는 데에서 차이가 있다. 

Receiver란, **해당 메소드가 어떤 타입의 메소드인지 명시하는 것**을 의미한다.

위 코드에서 `p` 에 해당하는 부분이 receiver이다. 보통 receiver의 명칭은 해당 타입의 첫 글자로 나타낸다.

즉, 위 코드에서의 `String()` 메소드는 `person` 이라는 타입의 메소드인 것이다.

메소드와 함수 선언의 또다른 **차이점**은 **선언 범위**이다. 함수는 어떤 블록 내부에서도 정의될 수 있지만, **메소드의 경우 package의 블록 레벨에서만 정의될 수 있다.**

메소드는 같은 이름의 다른 매개변수로 여러 개를 정의할 수 있지만 **같은 이름, 같은 매개변수로 다른 코드를 정의하여 여러 개의 메소드를 정의(Overload)할 수는 없다.**

이는 코드가 어떤 역할을 하는지 분명하게 해야 한다는 Go언어의 철학이다.

#### Pointer Receiver, Value Receiver

함수에서는 매개변수로 값을 전달하면 값이 복사 되어 기존의 값이 함수에 의해 수정되지 않기 때문에, **값 자체의 수정이 이루어질 수 있도록 포인터 타입을 매개변수로 사용**한다. **(Call by Reference)**

Receiver 또한 마찬가지로 **pointer receiver**와 **value receiver**가 존재한다.

다음 상황에 따라 어떤 타입의 receiver를 사용할지 고려할 수 있다.

- 메소드 내에서 recevier를 **직접적으로 수정**할 경우, **pointer receiver**를 사용한다.
- 메소드가 **`nil` 인스턴스를 다루는 경우**, **pointer receiver**를 사용한다.
- 메소드 내에서 recevier를 **직접적으로 수정**할 필요가 없을 경우, **value receiver**를 사용한다.

그리고 메소드 내에서 receiver를 수정할 필요가 없다고 하더라도 무작정 **value receiver**를 사용해서는 안된다.

만약, 동일한 타입을 receiver로 갖는 다른 메소드가 존재할 경우, 해당 메소드의 receiver는 pointer receiver일 것이고, 그 경우 **일관성을 보장하기 위해 모든 메소드에 대하여 pointer receiver를 사용**한다.

```go
// type 내부의 값이 직접적으로 변경되는 경우
func (c *Counter) Increment() {
	c.total++
	c.lastUpdated = time.Now()
}

// type 내부 값의 변경 없이 Read만 하는 경우
func (c Counter) String() string {
	return fmt.Sprintf("total: %d, last updated: %s", c.total, c.lastUpdated)
}
```

# 7. encoding/json

REST API는 서비스 간의 통신 수단으로 JSON 타입을 사용하는 것이 표준이다.

Go언어에서도 데이터 타입을 JSON 타입으로 변경하는 **encoding/json**이라는 표준 라이브러리를 제공한다.

이러한 변환을 marshal, unmarshal 이라고 한다.

- **marshal** : Go 언어의 타입을 인코딩 하는 것
- **unmarshal** : 디코딩 하여 다시 Go 언어의 타입으로 변환하는 것

### 7-1. json 태그

json으로 변환하기 위해서는 기본적으로 `struct` 타입을 사용한다.

아래와 같이 `struct` 타입 내의 각 필드 뒤에 **struct tag**를 추가한다.

> **struct tag**란, struct 필드에 메타 데이터를 붙이는 문자열을 의미한다. 
> 
> `tagName:"tagValue"` 형식으로 구성되어있다.
>
> 기본적으로 백틱(`)으로 감싸는데, 문자열 타입으로 인식되어 컴파일러가 별도로 구조를 검증하지는 않지만, `go vet` 이 포맷 검증을 할 수 있다.


```go
type Order struct {
	ID          string `json:"id"`
	DateOrdered string `json:"date_ordered"`
	CustomerID  string `json:"customer_id"`
	Items       []Item `json:"items"`
}

type Item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
```

위와 같이 `json:"id"` 라고 정의할 경우, 해당 필드를 JSON으로 변환했을 때의 필드 값은 `id`가 되는 것이다.

만약, 별도로 json 태그가 제공되지 않을 경우, 선언된 struct 필드의 이름을 기준으로 이름이 지정된다.

```go
type User struct{
	Name   string  //json 태그가 없을 경우
}

// 아래와 같이 struct 필드의 이름을 따른다.
{
  "Name" : "Jin"
}
```

만약, json 태그가 존재하지 않는 필드가 JSON에서 Go 언어 타입으로 unmarshaling이 진행된다면 대소문자를 무시하고, Go 언어 타입이 다시 JSON으로 marshaling이 될 경우, 맨 앞 글자를 대문자로 변경한다.

> 필드 이름이 소문자로 시작하면 unexported 즉, 같은 패키지 안에서만 접근 가능한 값이 되기 때문에 `encoding/json` 패키지에서 접근하지 못하게 된다.


만약, JSON으로 변환되지 않고 struct 내부에만 필드가 존재해야 할 경우, ``json:"-"`` 과 같이 `-` 를 사용한다.

필드를 optional 즉, 값이 존재하지 않을 때만 JSON에서 제거 되도록 설정하고 싶은 경우, ``json:"~,omitempty"`` 와 같이 뒤에 `,omitempty` 를 추가한다.

---

## 정리

Go언어 타입과 JSON 타입 간의 변환 과정은 `encoding/json` 패키지를 통해 지원되는데, 과정은 다음과 같다.

1. struct 내부 필드에 struct tag를 통해 json 필드임을 명시한다.
2. encoding/json 패키지가 struct 필드를 확인한다.
3. 각 필드의 json 필드를 확인하고, 태그가 존재할 경우, 태그 이름을 JSON key로 사용한다.
4. 태그가 없으면 필드의 이름을 JSON key로 사용한다.
5. 이후에 JSON 문자열/바이트 배열을 생성한다.

---

### 참고 자료

https://pkg.go.dev/cmd/go#hdr-Compile_packages_and_dependencies

https://pkg.go.dev/encoding/json?utm_source=chatgpt.com
