# 프로젝트 실습 진행 간 NS 설정 및 규칙

### **팀별 Namespace**

1. **사용 규칙**
    - 팀마다 **전용 Namespace 1개**를 사용합니다.
    - Operator, CR, Deployment 등 **모든 실습 리소스는 자기 팀 NS 안에서만** 작업해 주세요.
2. **팀별 Namespace 이름**
    - 1팀 (Database): `team-1`
    - 2팀 (KafkaTopic): `team-2`
    - 3팀 (Airflow): `team-3`
    - 4팀 (Observability): `team-4`
    - 5팀 (CanaryRelease): `team-5`
3. **공용 Namespace**
    - `platform` — Ingress 등 **5팀 공유 인프라**만 사용 (팀별 앱·Operator는 넣지 않음)
4. **Namespace 라벨**
    - `study.dev/team: "1"` ~ `"5"`

---

### **접속 권한 (중요)**

1. **현재 권한**
    - 배포된 kubeconfig는 **클러스터 관리자(cluster-admin) 권한**입니다.
    - 기술적으로는 모든 Namespace에 접근할 수 있습니다.
2. **지켜 주실 규칙**
    - ✅ **자기 팀 Namespace에서만** 작업 (`n team-N` 확인)
    - ✅ 기본 namespace를 자기 팀으로 설정 권장`kubectl config set-context --current --namespace=team-N`
    - ❌ 다른 팀 NS 수정·삭제 금지
    - ❌ CRD 등 클러스터 전역 리소스 임의 삭제 금지
    - ✅ 실습·데모 후 불필요한 리소스 정리

---

### **API Group (팀별 충돌 방지)**

1. **사용 규칙**
    - CRD는 클러스터 전역이므로 **API Group은 팀마다 겹치지 않게** 사용해 주세요.
2. **팀별 API Group (예시)**
    - 1팀: `database.study.dev`
    - 2팀: `kafka.study.dev`
    - 3팀: `airflow.study.dev`
    - 4팀: `observability.study.dev`
    - 5팀: `deploy.study.dev`