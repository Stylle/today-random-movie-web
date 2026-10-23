from pathlib import Path

import yaml


ROOT = Path(__file__).resolve().parents[1]


def check_compose(name: str, expect_build: bool) -> None:
    data = yaml.safe_load((ROOT / name).read_text(encoding="utf-8"))
    service = data["services"]["movie"]
    assert bool(service.get("build")) is expect_build
    assert service["ports"][0].startswith("${BIND_ADDRESS:-127.0.0.1}")
    assert "./data:/data" in service["volumes"]
    assert service["read_only"] is True
    assert "ALL" in service["cap_drop"]
    assert "no-new-privileges:true" in service["security_opt"]
    assert "SETUP_TOKEN" in service["environment"]
    assert "TRUSTED_PROXIES" in service["environment"]


def main() -> None:
    check_compose("docker-compose.yml", True)
    check_compose("docker-compose.hub.yml", False)
    dockerfile = (ROOT / "Dockerfile").read_text(encoding="utf-8")
    assert "USER 10001:10001" in dockerfile
    assert "HEALTHCHECK" in dockerfile
    assert "CGO_ENABLED=0" in dockerfile
    print("compose and Dockerfile security checks passed")


if __name__ == "__main__":
    main()
