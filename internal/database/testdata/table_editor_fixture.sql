CREATE TABLE customers(
    customer_id BIGINT PRIMARY KEY,
    name VARCHAR NOT NULL,
    email VARCHAR UNIQUE,
    region VARCHAR,
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE products(
    product_id BIGINT PRIMARY KEY,
    sku VARCHAR NOT NULL UNIQUE,
    name VARCHAR NOT NULL,
    price DECIMAL(12, 2) NOT NULL DEFAULT 0
);

CREATE TABLE orders(
    order_id BIGINT PRIMARY KEY,
    customer_id BIGINT NOT NULL REFERENCES customers(customer_id),
    status VARCHAR NOT NULL DEFAULT 'pending',
    ordered_at TIMESTAMP NOT NULL
);

CREATE TABLE order_items(
    order_id BIGINT NOT NULL,
    product_id BIGINT NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    unit_price DECIMAL(12, 2) NOT NULL,
    PRIMARY KEY (order_id, product_id),
    FOREIGN KEY (order_id) REFERENCES orders(order_id),
    FOREIGN KEY (product_id) REFERENCES products(product_id)
);

CREATE TABLE campaigns(
    campaign_id BIGINT PRIMARY KEY,
    name VARCHAR NOT NULL,
    start_date DATE NOT NULL
);

COMMENT ON TABLE customers IS 'Table editor fixture: customer master data.';
COMMENT ON COLUMN customers.region IS 'Optional sales region.';

INSERT INTO customers VALUES
    (1, 'Ada Lovelace', 'ada@example.com', 'East', TIMESTAMP '2026-01-02 09:00:00'),
    (2, 'Grace Hopper', 'grace@example.com', 'North', TIMESTAMP '2026-01-03 10:15:00'),
    (3, 'Alan Turing', 'alan@example.com', NULL, TIMESTAMP '2026-01-04 11:30:00'),
    (4, 'Edsger Dijkstra', 'edsger@example.com', 'West', TIMESTAMP '2026-01-05 13:00:00'),
    (5, 'Barbara Liskov', 'barbara@example.com', 'South', TIMESTAMP '2026-01-06 14:45:00');

INSERT INTO products VALUES
    (101, 'SKU-KEYBOARD', 'Mechanical Keyboard', 89.90),
    (102, 'SKU-MOUSE', 'Precision Mouse', 49.50),
    (103, 'SKU-MONITOR', '27-inch Monitor', 329.00),
    (104, 'SKU-DOCK', 'USB-C Dock', 159.00);

INSERT INTO orders VALUES
    (1001, 1, 'paid', TIMESTAMP '2026-02-01 08:30:00'),
    (1002, 2, 'pending', TIMESTAMP '2026-02-02 09:20:00'),
    (1003, 1, 'shipped', TIMESTAMP '2026-02-03 10:40:00'),
    (1004, 4, 'cancelled', TIMESTAMP '2026-02-04 11:10:00');

INSERT INTO order_items VALUES
    (1001, 101, 2, 89.90),
    (1001, 102, 1, 49.50),
    (1002, 103, 2, 329.00),
    (1002, 104, 1, 159.00),
    (1003, 101, 1, 89.90),
    (1003, 104, 1, 159.00),
    (1004, 102, 3, 49.50);

INSERT INTO campaigns VALUES
    (501, 'Spring Launch', DATE '2026-03-01'),
    (502, 'Summer Preview', DATE '2026-06-01'),
    (503, 'Autumn Upgrade', DATE '2026-09-01');
