const updates = new EventSource('/customers/watch');

updates.onerror = () => {
  document.querySelector('#customer-message').textContent = 'Connection lost. Reconnecting…';
};

updates.onopen = () => {
  const message = document.querySelector('#customer-message');
  if (message.textContent === 'Connection lost. Reconnecting…') message.textContent = '';
};

updates.onmessage = (event) => {
  document.querySelector('#customer-list').outerHTML = JSON.parse(event.data);
};

updates.addEventListener('customer-error', (event) => {
  document.querySelector('#customer-message').textContent = event.data;
});
