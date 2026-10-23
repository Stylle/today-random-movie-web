from pathlib import Path

import yaml


ROOT = Path(__file__).resolve().parents[1]


def check_compose(name: str, expect_build: bool) -> None:
    data = yaml.safe_load((ROOT / name).read_text(encoding="utf-8"))
    service = data["services"]["movie"]
    assert bool(service.get("build")) is expect_build
    assert service["container_name"] == "today-random-movie"
    assert service["restart"] == "always"
    assert service["ports"] == ["8080:8080"]
    assert any(volume.endswith(":/data") for volume in service["volumes"])
    environment = {item.split("=", 1)[0] for item in service["environment"]}
    assert environment == {"USER", "PASSWORD"}


def main() -> None:
    check_compose("docker-compose.yml", True)
    check_compose("docker-compose.hub.yml", False)
    dockerfile = (ROOT / "Dockerfile").read_text(encoding="utf-8")
    assert "USER 0:0" in dockerfile
    assert "HEALTHCHECK" in dockerfile
    assert "CGO_ENABLED=0" in dockerfile
    print("compose and Dockerfile checks passed")


if __name__ == "__main__":
    main()
