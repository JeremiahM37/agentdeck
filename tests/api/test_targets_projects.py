def test_target_crud_and_probe(client):
    r = client.post("/api/targets", json={"name": "lxc-104", "kind": "mock",
                                          "host": "192.0.2.20", "user": "dev"})
    assert r.status_code == 201
    tid = r.json()["id"]
    # duplicate name
    assert client.post("/api/targets", json={"name": "lxc-104"}).status_code == 409

    r = client.post(f"/api/targets/{tid}/check")
    assert r.json()["status"] == "online"
    assert "mock" in r.json()["info_json"]

    r = client.post("/api/projects", json={"name": "demo", "target_id": tid,
                                           "repo_path": "/opt/demo"})
    assert r.status_code == 201
    pid = r.json()["id"]
    assert client.post("/api/projects",
                       json={"name": "x", "target_id": 999,
                             "repo_path": "/x"}).status_code == 400

    # deletion guards
    assert client.delete(f"/api/targets/{tid}").status_code == 409
    assert client.delete(f"/api/projects/{pid}").status_code == 204
    assert client.delete(f"/api/targets/{tid}").status_code == 204


def test_mock_seed_present(client):
    names = [t["name"] for t in client.get("/api/targets").json()]
    assert "lxc-101-project-env" in names and "aiserver-local" in names
    assert len(client.get("/api/projects").json()) >= 2
