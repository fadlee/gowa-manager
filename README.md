# GOWA Manager

## Installation
To install dependencies:
```sh
bun install
```

## Running the application
To run in development mode:
```sh
bun run dev
```

Open http://localhost:3000

## Enabling Authentication
To enable basic authentication for the web UI:

1. Set the following environment variables:
   ```sh
   ADMIN_USERNAME=your_username
   ADMIN_PASSWORD=your_password
   ```

2. Or create a `.env` file with:
   ```
   ADMIN_USERNAME=admin
   ADMIN_PASSWORD=securepassword
   ```

3. Restart the application

When authentication is enabled, you'll be redirected to a login page when accessing the web UI.
The default credentials (if not set) are:
- Username: `admin`
- Password: `password`

## Development
To run both frontend and backend in watch mode:
```sh
bun run dev:all
```
