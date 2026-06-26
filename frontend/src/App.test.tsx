import { render, screen } from "@testing-library/react";
import App from "./App";

test("renders app shell heading", () => {
  render(<App />);
  expect(screen.getByText(/pit-pilot/i)).toBeInTheDocument();
});
