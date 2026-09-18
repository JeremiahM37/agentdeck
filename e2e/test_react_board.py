from pathlib import Path
import subprocess, time, urllib.request
ROOT=Path(__file__).parents[1]
def test_react_board_fixture(browser):
 p=subprocess.Popen(["npm","exec","vite","--","--host","127.0.0.1","--port","4187"],cwd=ROOT/"frontend",stdout=subprocess.DEVNULL,stderr=subprocess.STDOUT)
 try:
  for _ in range(50):
   try: urllib.request.urlopen("http://127.0.0.1:4187/react/board-harness.html");break
   except Exception: time.sleep(.1)
  with browser.new_context() as context:
   page=context.new_page();page.goto("http://127.0.0.1:4187/react/board-harness.html")
   page.get_by_text("Backlog job").wait_for();assert page.locator(".col").count()==6
   assert page.get_by_text("show 2 more").is_visible();page.get_by_text("show 2 more").click();assert page.get_by_text("Done 16").is_visible()
   page.get_by_text("Review job").click();page.get_by_text("Implemented safely").wait_for();page.get_by_text("⑂ A1",exact=False).click();page.get_by_text("± Diff").click();page.get_by_text("a.ts").wait_for();assert page.get_by_text("+new").is_visible()
   page.get_by_text("Timeline").click();page.get_by_text("✓ Mark done").click();page.wait_for_function("window.calls.some(x => String(x[0]).includes('/tasks/2/complete'))")
   page.get_by_label("Task details").get_by_role("button", name="✕").click()
   page.get_by_text("+ New task").click();page.get_by_label("Title").fill("New feature");page.get_by_role("button",name="Dispatch to board").click();page.wait_for_function("window.calls.some(x => x[0] === 'create')")
   page.get_by_text("Routines").click();page.get_by_text("PR sweep").wait_for();page.get_by_text("▶ Run now").click();page.wait_for_function("window.calls.some(x => String(x[0]).includes('/routines/1/run'))")
 finally:p.terminate();p.wait(timeout=5)
